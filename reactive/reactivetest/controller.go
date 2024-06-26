package reactivetest

import (
	"context"

	"github.com/kralicky/codegen/pkg/flagutil"
	"github.com/kralicky/protoconfig/reactive"
	"github.com/kralicky/protoconfig/server"
	"github.com/kralicky/protoconfig/storage/inmemory"
	"github.com/kralicky/protoconfig/util"
)

type config[T any] interface {
	server.ConfigType[T]
	flagutil.FlagSetter
}

type InMemoryControllerOptions[T config[T]] struct {
	existingActive  T
	existingDefault T
	setActive       T
	setDefault      T
}

type InMemoryControllerOption[T config[T]] func(*InMemoryControllerOptions[T])

func (o *InMemoryControllerOptions[T]) apply(opts ...InMemoryControllerOption[T]) {
	for _, op := range opts {
		op(o)
	}
}

func WithExistingActiveConfig[T config[T]](activeConfig T) InMemoryControllerOption[T] {
	return func(o *InMemoryControllerOptions[T]) {
		o.existingActive = activeConfig
	}
}

func WithExistingDefaultConfig[T config[T]](defaultConfig T) InMemoryControllerOption[T] {
	return func(o *InMemoryControllerOptions[T]) {
		o.existingDefault = defaultConfig
	}
}

func WithInitialActiveConfig[T config[T]](activeConfig T) InMemoryControllerOption[T] {
	return func(o *InMemoryControllerOptions[T]) {
		o.setActive = activeConfig
	}
}

func WithInitialDefaultConfig[T config[T]](defaultConfig T) InMemoryControllerOption[T] {
	return func(o *InMemoryControllerOptions[T]) {
		o.setDefault = defaultConfig
	}
}

func InMemoryController[T config[T]](opts ...InMemoryControllerOption[T]) (*reactive.Controller[T], context.Context, context.CancelFunc) {
	options := InMemoryControllerOptions[T]{}
	options.apply(opts...)

	ctx, ca := context.WithCancel(context.Background())
	defaultStore := inmemory.NewValueStore[T](util.ProtoClone)
	activeStore := inmemory.NewValueStore[T](util.ProtoClone)

	if options.existingDefault.ProtoReflect().IsValid() {
		if err := defaultStore.Put(ctx, options.existingDefault); err != nil {
			panic(err)
		}
	}

	if options.existingActive.ProtoReflect().IsValid() {
		if err := activeStore.Put(ctx, options.existingActive); err != nil {
			panic(err)
		}
	}

	tracker := server.NewDefaultingConfigTracker(defaultStore, activeStore, flagutil.LoadDefaults)
	ctrl := reactive.NewController[T](tracker)
	if err := ctrl.Start(ctx); err != nil {
		panic(err)
	}

	if options.setDefault.ProtoReflect().IsValid() {
		if err := tracker.SetDefault(ctx, options.setDefault); err != nil {
			panic(err)
		}
	}

	if options.setActive.ProtoReflect().IsValid() {
		if err := tracker.Apply(ctx, options.setActive); err != nil {
			panic(err)
		}
	}

	return ctrl, ctx, ca
}
