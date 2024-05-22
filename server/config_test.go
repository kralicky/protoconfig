package server_test

import (
	"context"

	"github.com/kralicky/protoconfig/server"
	"github.com/kralicky/protoconfig/storage"
	"github.com/kralicky/protoconfig/storage/inmemory"
	conformance_server "github.com/kralicky/protoconfig/test/conformance/server"
	"github.com/kralicky/protoconfig/test/ext"
	"github.com/kralicky/protoconfig/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func newValueStore() storage.ValueStoreT[*ext.SampleConfiguration] {
	return inmemory.NewValueStore[*ext.SampleConfiguration](util.ProtoClone)
}

func newKeyValueStore() storage.KeyValueStoreT[*ext.SampleConfiguration] {
	return inmemory.NewKeyValueStore[*ext.SampleConfiguration](util.ProtoClone)
}

var _ = Describe("Defaulting Config Tracker", Label("unit"), conformance_server.DefaultingConfigTrackerTestSuite(newValueStore, newValueStore))

type testContextKey struct {
	*ext.SampleGetRequest
}

func (t testContextKey) ContextKey() protoreflect.FieldDescriptor {
	return t.ProtoReflect().Descriptor().Fields().ByName("key")
}

var _ = Describe("Context Keys", func() {
	It("should correctly obtain context key values from ContextKeyable messages", func() {
		getReq := &ext.SampleGetRequest{
			Key: lo.ToPtr("foo"),
		}
		ctx := context.Background()
		ctx = server.ContextWithKey(ctx, getReq)
		key := server.KeyFromContext(ctx)

		Expect(key).To(Equal("foo"))
	})
	It("should correctly obtain context key values if the key field is a core.Reference", func() {
		testKeyable := &testContextKey{
			SampleGetRequest: &ext.SampleGetRequest{
				Key: lo.ToPtr("foo"),
			},
		}
		ctx := context.Background()
		ctx = server.ContextWithKey(ctx, testKeyable)
		key := server.KeyFromContext(ctx)

		Expect(key).To(Equal("foo"))
	})
})
