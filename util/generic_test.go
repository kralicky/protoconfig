package util_test

import (
	"errors"
	"unsafe"

	"github.com/kralicky/protoconfig/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var _ = Describe("Generic Utils", Label("unit"), func() {
	Context("Must", func() {
		It("should panic if a non-nil error is given", func() {
			Expect(func() {
				util.Must("test", errors.New("test"))
			}).To(Panic())
			Expect(func() {
				util.Must(errors.New("test"))
			}).To(Panic())
			Expect(func() {
				util.Must(1, errors.New("test"), errors.New("test"))
			}).To(Panic())
			Expect(func() {
				util.Must(1, nil)
			}).NotTo(Panic())
		})
	})
	Context("DeepCopy", func() {
		It("should copy a struct", func() {
			type testStruct struct {
				Field1 *string
				Field2 *int
			}
			ts := &testStruct{
				Field1: lo.ToPtr("test"),
				Field2: lo.ToPtr(1),
			}
			ts2 := &testStruct{}
			util.DeepCopyInto(ts2, ts)
			Expect(ts2).To(Equal(ts))
			Expect(uintptr(unsafe.Pointer(ts.Field1))).NotTo(Equal(uintptr(unsafe.Pointer(ts2.Field1))))
			Expect(uintptr(unsafe.Pointer(ts.Field2))).NotTo(Equal(uintptr(unsafe.Pointer(ts2.Field2))))

			ts3 := util.DeepCopy(ts)
			Expect(ts3).To(Equal(ts))
			Expect(uintptr(unsafe.Pointer(ts.Field1))).NotTo(Equal(uintptr(unsafe.Pointer(ts3.Field1))))
			Expect(uintptr(unsafe.Pointer(ts.Field2))).NotTo(Equal(uintptr(unsafe.Pointer(ts3.Field2))))
		})
	})
})
