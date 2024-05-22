package future_test

import (
	"runtime/debug"
	"time"

	"github.com/kralicky/protoconfig/util/future"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Future", Label("unit"), func() {
	BeforeEach(func() {
		// temporarily pause garbage collection to avoid interfering with timing
		gcPercent := debug.SetGCPercent(-1)
		DeferCleanup(func() {
			debug.SetGCPercent(gcPercent)
		})
	})

	Specify("Get should block until Set is called", func() {
		f := future.New[string]()
		go func() {
			time.Sleep(time.Millisecond * 100)
			f.Set("test")
		}()
		start := time.Now()
		Expect(f.Get()).To(Equal("test"))
		Expect(time.Since(start)).To(BeNumerically(">=", time.Millisecond*100))
	})
})
