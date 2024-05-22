package inmemory_test

import (
	"bytes"
	"testing"

	"github.com/kralicky/protoconfig/storage"
	"github.com/kralicky/protoconfig/storage/inmemory"
	conformance_storage "github.com/kralicky/protoconfig/test/conformance/storage"
	"github.com/kralicky/protoconfig/util/future"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestInmemory(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Inmemory Suite")
}

type testBroker struct{}

func (t testBroker) KeyValueStore(string) storage.KeyValueStore {
	return inmemory.NewKeyValueStore(bytes.Clone)
}

var _ = Describe("In-memory KV Store", Ordered, Label("integration"), conformance_storage.KeyValueStoreTestSuite(future.Instant(testBroker{}), conformance_storage.NewBytes, Equal))
