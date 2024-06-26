package dryrun

import (
	"context"
	"fmt"
	"path"
	"reflect"
	strings "strings"

	"github.com/bufbuild/protovalidate-go"
	cliutil "github.com/kralicky/codegen/pkg/cliutil"
	corev1 "github.com/kralicky/protoconfig/apis/core/v1"
	"github.com/kralicky/protoconfig/server"
	"github.com/nsf/jsondiff"
	"github.com/samber/lo"
	cobra "github.com/spf13/cobra"
	"github.com/ttacon/chalk"
	"google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Creates the 'dry-run' command for a compatible generated service client.
//
// The compiler can resolve all type parameters given the generated
// ClientContextInjector; it should not be necessary to specify them manually.
//
// The ClientContextInjector will be generated for any protobuf file containing
// the file option `option (cli.generator).generate = true;` and a compatible
// service definition.
//
// In a separate file in the same package as the generated code, enable the
// dry-run command as follows, substituting "X" for your service name:
//
//	func init() {
//	  addExtraXCmd(dryrun.BuildCmd("dry-run", XContextInjector),
//	    BuildXSetDefaultCmd(),
//	    BuildXSetCmd(),
//	    BuildXResetDefaultCmd(),
//	    BuildXResetCmd(),
//	  )
//	}
//
// The use line is the name of the dry-run command. If it is intended to be
// a generated subcommand, it can be multiple words (e.g. "config dry-run").
//
// Once the dry-run command is enabled, it will be available in the CLI
// as a subcommand of the service's top-level command. For example,
// (assuming the service's use line is "config"):
//
//	$ <cmd> x config set [--flags ...]
//	$ <cmd> x config dry-run set [--flags ...]
//	$ <cmd> x config reset [--flags ...]
//	$ <cmd> x config dry-run reset [--flags ...]
//	etc.
func BuildCmd[
	T server.ConfigType[T],
	G server.GetRequestType,
	S server.SetRequestType[T],
	R server.ResetRequestType[T],
	D server.DryRunRequestType[T],
	DR server.DryRunResponseType[T],
	H server.HistoryRequestType,
	HR server.HistoryResponseType[T],
	I server.ClientContextInjector[C],
	C interface {
		server.GetClient[T, G]
		server.SetClient[T, S]
		server.ResetClient[T, R]
		server.DryRunClient[T, D, DR]
		server.HistoryClient[T, H, HR]
	},
](use string, cci I, dryRunnableCmds ...*cobra.Command) *cobra.Command {
	var diffFull bool
	var diffFormat string
	dryRunCmd := &cobra.Command{
		Use: use,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := cliutil.BasePreRunE(cmd, args); err != nil {
				return err
			}
			// inject the dry-run client into the context
			client, ok := cci.ClientFromContext(cmd.Context())
			if ok {
				cmd.SetContext(cci.ContextWithClient(cmd.Context(), cci.NewClient(NewDryRunClient(client).AsClientConn(cci))))
			}
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if err := cliutil.BasePostRunE(cmd, args); err != nil {
				return err
			}
			// print the dry-run response
			client, ok := cci.ClientFromContext(cmd.Context())
			if !ok {
				return nil
			}
			dryRunClient := NewDryRunClient(client).FromClientConn(cci.UnderlyingConn(client))

			response := dryRunClient.Response()
			if errs := (*protovalidate.ValidationError)(response.GetValidationErrors()); errs != nil {
				cmd.Println(chalk.Yellow.Color(errs.Error()))
			}

			var opts jsondiff.Options
			switch diffFormat {
			case "console":
				opts = jsondiff.DefaultConsoleOptions()
			case "json":
				opts = jsondiff.DefaultJSONOptions()
			case "html":
				opts = jsondiff.DefaultHTMLOptions()
			default:
				return fmt.Errorf("invalid diff format: %s", diffFormat)
			}
			opts.SkipMatches = !diffFull

			str, anyChanges := server.RenderJsonDiff(response.GetCurrent(), response.GetModified(), opts)
			if !anyChanges {
				cmd.Println("no changes")
			} else {
				cmd.Println(str)
			}
			return nil
		},
	}
	dryRunCmd.PersistentFlags().BoolVar(&diffFull, "diff-full", false, "show full diff, including all unchanged fields")
	dryRunCmd.PersistentFlags().StringVar(&diffFormat, "diff-format", "console", "diff format (console, json, html)")

	// if all commands have multiple words with the same first word, trim the first word
	maybeParentCommand := ""
	for _, cmd := range dryRunnableCmds {
		if words := strings.SplitAfter(cmd.Use, " "); len(words) > 1 {
			if maybeParentCommand == "" || maybeParentCommand == words[0] {
				maybeParentCommand = words[0]
			} else {
				maybeParentCommand = ""
				break
			}
		}
	}
	for _, cmd := range dryRunnableCmds {
		cmd.Use = strings.TrimPrefix(cmd.Use, maybeParentCommand)
		cmd.Short = fmt.Sprintf("[dry-run] %s", cmd.Short)
		dryRunCmd.AddCommand(cmd)
	}
	return dryRunCmd
}

func NewDryRunClient[
	T server.ConfigType[T],
	G server.GetRequestType,
	S server.SetRequestType[T],
	R server.ResetRequestType[T],
	D server.DryRunRequestType[T],
	DR server.DryRunResponseType[T],
	H server.HistoryRequestType,
	HR server.HistoryResponseType[T],
	C interface {
		server.GetClient[T, G]
		server.SetClient[T, S]
		server.ResetClient[T, R]
		server.DryRunClient[T, D, DR]
		server.HistoryClient[T, H, HR]
	},
](client C) *DryRunClient[T, G, S, R, D, DR, H, HR, C] {
	return &DryRunClient[T, G, S, R, D, DR, H, HR, C]{
		client:         client,
		maskedFields:   corev1.MaskedFields[T](),
		contextKeyable: reflect.TypeFor[D]().Implements(reflect.TypeFor[server.ContextKeyable]()),
	}
}

type DryRunClient[
	T server.ConfigType[T],
	G server.GetRequestType,
	S server.SetRequestType[T],
	R server.ResetRequestType[T],
	D server.DryRunRequestType[T],
	DR server.DryRunResponseType[T],
	H server.HistoryRequestType,
	HR server.HistoryResponseType[T],
	C interface {
		server.GetClient[T, G]
		server.SetClient[T, S]
		server.ResetClient[T, R]
		server.DryRunClient[T, D, DR]
		server.HistoryClient[T, H, HR]
	},
] struct {
	client   C
	request  D
	response DR

	maskedFields   []protoreflect.FieldDescriptor
	contextKeyable bool
}

func (dc *DryRunClient[T, G, S, R, D, DR, H, HR, C]) AsClientConn(cci server.ClientContextInjector[C]) grpc.ClientConnInterface {
	return NewDryRunClientShim(dc, cci)
}

func (*DryRunClient[T, G, S, R, D, DR, H, HR, C]) FromClientConn(cc grpc.ClientConnInterface) *DryRunClient[T, G, S, R, D, DR, H, HR, C] {
	return cc.(*DryRunClientShim[T, G, S, R, D, DR, H, HR, C]).dr
}

func (dc *DryRunClient[T, G, S, R, D, DR, H, HR, C]) Response() DR {
	return dc.response
}

// Reset implements server.GetClient[T, G].
func (dc *DryRunClient[T, G, S, R, D, DR, H, HR, C]) Reset(ctx context.Context, req R, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	dc.request = NewDryRunRequest[T, D]().
		Active().
		Reset().
		Revision(req.GetRevision()).
		Patch(req.GetPatch()).
		Mask(req.GetMask()).
		Build()

	if dc.contextKeyable {
		copyContextKey(dc.request, req)
	}

	var err error
	dc.response, err = dc.client.DryRun(ctx, dc.request, opts...)
	if err != nil {
		return nil, fmt.Errorf("[dry-run] error: %w", err)
	}
	return &emptypb.Empty{}, nil
}

// ResetDefault implements server.ResetClient[T, R].
func (dc *DryRunClient[T, G, S, R, D, DR, H, HR, C]) ResetDefault(ctx context.Context, _ *emptypb.Empty, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	dc.request = NewDryRunRequest[T, D]().
		Default().
		Reset().
		Build()

	var err error
	dc.response, err = dc.client.DryRun(ctx, dc.request, opts...)
	if err != nil {
		return nil, fmt.Errorf("[dry-run] error: %w", err)
	}
	return &emptypb.Empty{}, nil
}

// Set implements server.SetClient[T, S].
func (dc *DryRunClient[T, G, S, R, D, DR, H, HR, C]) Set(ctx context.Context, in S, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	if in.GetSpec().ProtoReflect().IsValid() {
		for _, mf := range dc.maskedFields {
			in.GetSpec().ProtoReflect().Clear(mf)
		}
	}
	dc.request = NewDryRunRequest[T, D]().
		Active().
		Set().
		Spec(in.GetSpec()).
		Build()

	if dc.contextKeyable {
		copyContextKey(dc.request, in)
	}

	var err error
	dc.response, err = dc.client.DryRun(ctx, dc.request, opts...)
	if err != nil {
		return nil, fmt.Errorf("[dry-run] error: %w", err)
	}
	return &emptypb.Empty{}, nil
}

// SetDefault implements server.SetClient[T, S].
func (dc *DryRunClient[T, G, S, R, D, DR, H, HR, C]) SetDefault(ctx context.Context, in S, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	if in.GetSpec().ProtoReflect().IsValid() {
		for _, mf := range dc.maskedFields {
			in.GetSpec().ProtoReflect().Clear(mf)
		}
	}
	dc.request = NewDryRunRequest[T, D]().
		Default().
		Set().
		Spec(in.GetSpec()).
		Build()

	var err error
	dc.response, err = dc.client.DryRun(ctx, dc.request, opts...)
	if err != nil {
		return nil, fmt.Errorf("[dry-run] error: %w", err)
	}
	return &emptypb.Empty{}, nil
}

// Get implements server.GetClient[T, G].
func (dc *DryRunClient[T, G, S, R, D, DR, H, HR, C]) Get(ctx context.Context, in G, opts ...grpc.CallOption) (T, error) {
	return dc.client.Get(ctx, in, opts...)
}

// GetDefault implements server.GetClient[T, G].
func (dc *DryRunClient[T, G, S, R, D, DR, H, HR, C]) GetDefault(ctx context.Context, in G, opts ...grpc.CallOption) (T, error) {
	return dc.client.GetDefault(ctx, in, opts...)
}

// DryRun implements server.DryRunClient[T, D, DR].
func (dc *DryRunClient[T, G, S, R, D, DR, H, HR, C]) DryRun(_ context.Context, _ D, _ ...grpc.CallOption) (DR, error) {
	return lo.Empty[DR](), status.Errorf(codes.Unimplemented, "[dry-run] method DryRun not implemented")
}

// History implements server.HistoryClient[T, H, HR].
func (dc *DryRunClient[T, G, S, R, D, DR, H, HR, C]) History(_ context.Context, _ H, _ ...grpc.CallOption) (HR, error) {
	return lo.Empty[HR](), status.Errorf(codes.Unimplemented, "[dry-run] method History not implemented")
}

func copyContextKey(dst, src proto.Message) {
	dst.ProtoReflect().Set(dst.(server.ContextKeyable).ContextKey(),
		src.ProtoReflect().Get(src.(server.ContextKeyable).ContextKey()))
}

type DryRunClientShim[
	T server.ConfigType[T],
	G server.GetRequestType,
	S server.SetRequestType[T],
	R server.ResetRequestType[T],
	D server.DryRunRequestType[T],
	DR server.DryRunResponseType[T],
	H server.HistoryRequestType,
	HR server.HistoryResponseType[T],
	C interface {
		server.GetClient[T, G]
		server.SetClient[T, S]
		server.ResetClient[T, R]
		server.DryRunClient[T, D, DR]
		server.HistoryClient[T, H, HR]
	},
] struct {
	cc grpc.ClientConnInterface
	dr *DryRunClient[T, G, S, R, D, DR, H, HR, C]
}

func NewDryRunClientShim[
	T server.ConfigType[T],
	G server.GetRequestType,
	S server.SetRequestType[T],
	R server.ResetRequestType[T],
	D server.DryRunRequestType[T],
	DR server.DryRunResponseType[T],
	H server.HistoryRequestType,
	HR server.HistoryResponseType[T],
	C interface {
		server.GetClient[T, G]
		server.SetClient[T, S]
		server.ResetClient[T, R]
		server.DryRunClient[T, D, DR]
		server.HistoryClient[T, H, HR]
	},
](
	dr *DryRunClient[T, G, S, R, D, DR, H, HR, C],
	cci server.ClientContextInjector[C],
) grpc.ClientConnInterface {
	return &DryRunClientShim[T, G, S, R, D, DR, H, HR, C]{
		dr: dr,
		cc: cci.UnderlyingConn(dr.client),
	}
}

// Invoke implements grpc.ClientConnInterface.
func (dc *DryRunClientShim[T, G, S, R, D, DR, H, HR, C]) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	switch path.Base(method) {
	case "GetDefault":
		resp, err := dc.dr.GetDefault(ctx, args.(G), opts...)
		if err != nil {
			return err
		}
		proto.Merge(reply.(proto.Message), resp)
	case "SetDefault":
		resp, err := dc.dr.SetDefault(ctx, args.(S), opts...)
		if err != nil {
			return err
		}
		proto.Merge(reply.(proto.Message), resp)
	case "ResetDefault":
		resp, err := dc.dr.ResetDefault(ctx, args.(*emptypb.Empty), opts...)
		if err != nil {
			return err
		}
		proto.Merge(reply.(proto.Message), resp)
	case "Get":
		resp, err := dc.dr.Get(ctx, args.(G), opts...)
		if err != nil {
			return err
		}
		proto.Merge(reply.(proto.Message), resp)
	case "Set":
		resp, err := dc.dr.Set(ctx, args.(S), opts...)
		if err != nil {
			return err
		}
		proto.Merge(reply.(proto.Message), resp)
	case "Reset":
		resp, err := dc.dr.Reset(ctx, args.(R), opts...)
		if err != nil {
			return err
		}
		proto.Merge(reply.(proto.Message), resp)
	case "DryRun":
		return status.Errorf(codes.Unimplemented, "[dry-run] attempted to recursively invoke DryRun")
	default:
		return dc.cc.Invoke(ctx, method, args, reply, opts...)
	}
	return nil
}

// NewStream implements grpc.ClientConnInterface.
func (dc *DryRunClientShim[T, G, S, R, D, DR, H, HR, C]) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return dc.cc.NewStream(ctx, desc, method, opts...)
}
