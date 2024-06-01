package dryrun

import (
	"reflect"

	corev1 "github.com/kralicky/protoconfig/apis/core/v1"
	"github.com/kralicky/protoconfig/server"
	"github.com/kralicky/protoconfig/util"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func NewDryRunRequest[
	T server.ConfigType[T],
	D server.DryRunRequestType[T],
]() DryRunRequestBuilder[T, D] {
	return &dryRunRequestBuilderImpl[T, D]{
		request: util.NewMessage[D](),
	}
}

type DryRunRequestBuilder[
	T server.ConfigType[T],
	D server.DryRunRequestType[T],
] interface {
	Default() DryRunRequestBuilder_Default[T, D]
	Active() DryRunRequestBuilder_Active[T, D]
}

type DryRunRequestBuilder_Active[
	T server.ConfigType[T],
	D server.DryRunRequestType[T],
] interface {
	Set() DryRunRequestBuilder_Set[T, D]
	Reset() DryRunRequestBuilder_Reset[T, D]
}

type DryRunRequestBuilder_Default[
	T server.ConfigType[T],
	D server.DryRunRequestType[T],
] interface {
	Set() DryRunRequestBuilder_Set[T, D]
	Reset() DryRunRequestBuilder_Build[T, D]
}

type DryRunRequestBuilder_Set[
	T server.ConfigType[T],
	D server.DryRunRequestType[T],
] interface {
	Spec(spec T) DryRunRequestBuilder_Build[T, D]
}

type DryRunRequestBuilder_Reset[
	T server.ConfigType[T],
	D server.DryRunRequestType[T],
] interface {
	Revision(rev *corev1.Revision) DryRunRequestBuilder_ResetOrBuild[T, D]
	Patch(patch T) DryRunRequestBuilder_ResetOrBuild[T, D]
	Mask(mask *fieldmaskpb.FieldMask) DryRunRequestBuilder_ResetOrBuild[T, D]
}

type DryRunRequestBuilder_Build[
	T server.ConfigType[T],
	D server.DryRunRequestType[T],
] interface {
	Build() D
}

type DryRunRequestBuilder_ResetOrBuild[
	T server.ConfigType[T],
	D server.DryRunRequestType[T],
] interface {
	DryRunRequestBuilder_Reset[T, D]
	DryRunRequestBuilder_Build[T, D]
}

type dryRunRequestBuilderImpl[
	T server.ConfigType[T],
	D server.DryRunRequestType[T],
] struct {
	request D
}

type dryRunRequestBuilderImpl_ResetDefault[
	T server.ConfigType[T],
	D server.DryRunRequestType[T],
] struct {
	*dryRunRequestBuilderImpl[T, D]
}

func (b dryRunRequestBuilderImpl_ResetDefault[T, D]) Reset() DryRunRequestBuilder_Build[T, D] {
	b.dryRunRequestBuilderImpl.Reset()
	return b.dryRunRequestBuilderImpl
}

func (b *dryRunRequestBuilderImpl[T, D]) Mask(mask *fieldmaskpb.FieldMask) DryRunRequestBuilder_ResetOrBuild[T, D] {
	if mask != nil {
		b.request.ProtoReflect().Set(util.FieldByName[D]("mask"), protoreflect.ValueOfMessage(mask.ProtoReflect()))
	}
	return b
}

func (b *dryRunRequestBuilderImpl[T, D]) Patch(patch T) DryRunRequestBuilder_ResetOrBuild[T, D] {
	if !reflect.ValueOf(patch).IsNil() {
		b.request.ProtoReflect().Set(util.FieldByName[D]("patch"), protoreflect.ValueOfMessage(patch.ProtoReflect()))
	}
	return b
}

func (b *dryRunRequestBuilderImpl[T, D]) Revision(rev *corev1.Revision) DryRunRequestBuilder_ResetOrBuild[T, D] {
	if rev != nil {
		b.request.ProtoReflect().Set(util.FieldByName[D]("revision"), protoreflect.ValueOfMessage(rev.ProtoReflect()))
	}
	return b
}

func (b *dryRunRequestBuilderImpl[T, D]) Set() DryRunRequestBuilder_Set[T, D] {
	b.request.ProtoReflect().Set(util.FieldByName[D]("action"), protoreflect.ValueOfEnum(server.Action_Set.Number()))
	return b
}

func (b *dryRunRequestBuilderImpl[T, D]) Reset() DryRunRequestBuilder_Reset[T, D] {
	b.request.ProtoReflect().Set(util.FieldByName[D]("action"), protoreflect.ValueOfEnum(server.Action_Reset.Number()))
	return b
}

func (b *dryRunRequestBuilderImpl[T, D]) Default() DryRunRequestBuilder_Default[T, D] {
	b.request.ProtoReflect().Set(util.FieldByName[D]("target"), protoreflect.ValueOfEnum(server.Target_Default.Number()))
	return dryRunRequestBuilderImpl_ResetDefault[T, D]{b}
}

func (b *dryRunRequestBuilderImpl[T, D]) Active() DryRunRequestBuilder_Active[T, D] {
	b.request.ProtoReflect().Set(util.FieldByName[D]("target"), protoreflect.ValueOfEnum(server.Target_Active.Number()))
	return b
}

func (b *dryRunRequestBuilderImpl[T, D]) Spec(spec T) DryRunRequestBuilder_Build[T, D] {
	if !reflect.ValueOf(spec).IsNil() {
		b.request.ProtoReflect().Set(util.FieldByName[D]("spec"), protoreflect.ValueOfMessage(spec.ProtoReflect()))
	}
	return b
}

func (b *dryRunRequestBuilderImpl[T, D]) Build() D {
	return b.request
}
