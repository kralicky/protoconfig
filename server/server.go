package server

import (
	"context"

	"github.com/kralicky/protoconfig/storage"
	"github.com/kralicky/protoconfig/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// Implements a subset of methods usually required by a server which uses a DefaultingConfigTracker
// to manage its configuration. These implementations should not vary between drivers, so they are
// provided here as a convenience.
type BaseConfigServer[
	G GetRequestType,
	S SetRequestType[T],
	R ResetRequestType[T],
	H HistoryRequestType,
	HR HistoryResponseType[T],
	T ConfigType[T],
] struct {
	tracker *DefaultingConfigTracker[T]
}

// Returns a new instance of the BaseConfigServer with the defined type.
// Can be called from a nil pointer of the BaseConfigServer type.
// Example:
// var server *BaseConfigServer[...]
// server = server.Build()
func (*BaseConfigServer[G, S, R, H, HR, T]) Build(
	defaultStore, activeStore storage.ValueStoreT[T],
	loadDefaultsFunc DefaultLoaderFunc[T],
) *BaseConfigServer[G, S, R, H, HR, T] {
	return &BaseConfigServer[G, S, R, H, HR, T]{
		tracker: NewDefaultingConfigTracker[T](defaultStore, activeStore, loadDefaultsFunc),
	}
}

func (s *BaseConfigServer[G, S, R, H, HR, T]) Get(ctx context.Context, in G) (T, error) {
	return s.tracker.GetActiveOrDefault(ctx, in.GetRevision())
}

func (s *BaseConfigServer[G, S, R, H, HR, T]) GetDefault(ctx context.Context, in G) (T, error) {
	return s.tracker.GetDefault(ctx, in.GetRevision())
}

func (s *BaseConfigServer[G, S, R, H, HR, T]) ResetDefault(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if err := s.tracker.ResetDefault(ctx); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *BaseConfigServer[G, S, R, H, HR, T]) Reset(ctx context.Context, in R) (*emptypb.Empty, error) {
	// If T contains at least one masked field, ensure a non-nil mask is always
	// passed to ResetConfig. This ensures the active config is never deleted from
	// the underlying store, and therefore history is always preserved.
	if len(s.tracker.maskedFields) > 0 {
		if in.GetMask() == nil {
			in.ProtoReflect().Set(util.FieldByName[R]("mask"), protoreflect.ValueOfMessage(util.NewMessage[*fieldmaskpb.FieldMask]().ProtoReflect()))
		}
		var t T
		for _, maskedField := range s.tracker.maskedFields {
			if err := in.GetMask().Append(t, string(maskedField.Name())); err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "invalid mask: %s", err.Error())
			}
		}
		patch := in.GetPatch()
		for _, maskedField := range s.tracker.maskedFields {
			if patch.ProtoReflect().Has(maskedField) {
				// ensure the enabled field cannot be modified by the patch
				patch.ProtoReflect().Clear(maskedField)
			}
		}
	}
	if err := s.tracker.Reset(ctx, in.GetMask(), in.GetPatch()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *BaseConfigServer[G, S, R, H, HR, T]) Set(ctx context.Context, in S) (*emptypb.Empty, error) {
	s.clearMaskedFields(in.GetSpec())
	if err := s.tracker.Apply(ctx, in.GetSpec()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *BaseConfigServer[G, S, R, H, HR, T]) SetDefault(ctx context.Context, in S) (*emptypb.Empty, error) {
	s.clearMaskedFields(in.GetSpec())
	if err := s.tracker.SetDefault(ctx, in.GetSpec()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *BaseConfigServer[G, S, R, H, HR, T]) clearMaskedFields(t T) {
	for _, field := range s.tracker.maskedFields {
		if t.ProtoReflect().Has(field) {
			t.ProtoReflect().Clear(field)
		}
	}
}

func (s *BaseConfigServer[G, S, R, H, HR, T]) History(ctx context.Context, in H) (HR, error) {
	options := []storage.HistoryOpt{
		storage.IncludeValues(in.GetIncludeValues()),
	}
	if in.GetRevision() != nil {
		options = append(options, storage.WithRevision(in.GetRevision().GetRevision()))
	}
	revisions, err := s.tracker.History(ctx, in.GetTarget(), options...)
	resp := util.NewMessage[HR]()
	if err != nil {
		return resp, err
	}
	entries := resp.ProtoReflect().Mutable(util.FieldByName[HR]("entries")).List()
	for _, rev := range revisions {
		if in.GetIncludeValues() {
			spec := rev.Value()
			SetRevision(spec, rev.Revision(), rev.Timestamp())
			entries.Append(protoreflect.ValueOfMessage(spec.ProtoReflect()))
		} else {
			newSpec := util.NewMessage[T]()
			SetRevision(newSpec, rev.Revision(), rev.Timestamp())
			entries.Append(protoreflect.ValueOfMessage(newSpec.ProtoReflect()))
		}
	}
	return resp, nil
}

func (s *BaseConfigServer[G, S, R, H, HR, T]) Tracker() *DefaultingConfigTracker[T] {
	return s.tracker
}

// ServerDryRun sends a dry-run request to the tracker and returns the results
// in a generic wrapper type.
//
// Because DryRun is an optional config server API, the request and response
// types are not part of the collection of required type parameters for this
// server. The typed request can be passed directly to ServerDryRun using the
// generic interface DryRunRequestType[T], which it will already implement
// if the request is well-formed. The corresponding response type can't be
// known at compile-time, so the caller will need to translate the generic
// response type DryRunResults[T] into the correct response type for the rpc.
//
// This is typically most of the work required to implement a dry-run endpoint,
// but callers may wish to perform additional validation on the modified config
// before returning the dry-run response to the client using the typed dry-run
// response message.
//
// If the config type has validation rules defined using protovalidate, they
// will be run against the modified config and included in this response.
func (s *BaseConfigServer[G, S, R, H, HR, T]) ServerDryRun(ctx context.Context, req DryRunRequestType[T]) (DryRunResults[T], error) {
	return s.tracker.DryRun(ctx, req)
}

type ContextKeyableConfigServer[
	G interface {
		GetRequestType
		ContextKeyable
	},
	S interface {
		SetRequestType[T]
		ContextKeyable
	},
	R interface {
		ResetRequestType[T]
		ContextKeyable
	},
	H interface {
		HistoryRequestType
		ContextKeyable
	},
	HR HistoryResponseType[T],
	T ConfigType[T],
] struct {
	base *BaseConfigServer[G, S, R, H, HR, T]
}

// Returns a new instance of the ContextKeyableConfigServer with the defined type.
// This is a conveience function to avoid repeating the type parameters.
func (*ContextKeyableConfigServer[G, S, R, H, HR, T]) Build(
	defaultStore storage.ValueStoreT[T],
	activeStore storage.KeyValueStoreT[T],
	loadDefaultsFunc DefaultLoaderFunc[T],
) *ContextKeyableConfigServer[G, S, R, H, HR, T] {
	tracker := NewDefaultingActiveKeyedConfigTracker(
		defaultStore,
		activeStore,
		loadDefaultsFunc,
	)
	return &ContextKeyableConfigServer[G, S, R, H, HR, T]{
		base: &BaseConfigServer[G, S, R, H, HR, T]{
			tracker: tracker,
		},
	}
}

func (s *ContextKeyableConfigServer[G, S, R, H, HR, T]) GetDefault(ctx context.Context, in G) (T, error) {
	return s.base.GetDefault(ctx, in)
}

func (s *ContextKeyableConfigServer[G, S, R, H, HR, T]) ResetDefault(ctx context.Context, in *emptypb.Empty) (*emptypb.Empty, error) {
	return s.base.ResetDefault(ctx, in)
}

func (s *ContextKeyableConfigServer[G, S, R, H, HR, T]) SetDefault(ctx context.Context, in S) (*emptypb.Empty, error) {
	return s.base.SetDefault(ctx, in)
}

func (s *ContextKeyableConfigServer[G, S, R, H, HR, T]) Get(ctx context.Context, in G) (T, error) {
	return s.base.Get(contextWithKey(ctx, in), in)
}

func (s *ContextKeyableConfigServer[G, S, R, H, HR, T]) Reset(ctx context.Context, in R) (*emptypb.Empty, error) {
	return s.base.Reset(contextWithKey(ctx, in), in)
}

func (s *ContextKeyableConfigServer[G, S, R, H, HR, T]) Set(ctx context.Context, in S) (*emptypb.Empty, error) {
	return s.base.Set(contextWithKey(ctx, in), in)
}

func (s *ContextKeyableConfigServer[G, S, R, H, HR, T]) History(ctx context.Context, in H) (HR, error) {
	return s.base.History(contextWithKey(ctx, in), in)
}

func (s *ContextKeyableConfigServer[G, S, R, H, HR, T]) ServerDryRun(ctx context.Context, req interface {
	DryRunRequestType[T]
	ContextKeyable
},
) (DryRunResults[T], error) {
	return s.base.ServerDryRun(contextWithKey(ctx, req), req)
}

func (s *ContextKeyableConfigServer[G, S, R, H, HR, T]) InjectContextKey(ctx context.Context, in ContextKeyable) context.Context {
	return contextWithKey(ctx, in)
}
