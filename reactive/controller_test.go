package reactive_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"google.golang.org/protobuf/reflect/protopath"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/kralicky/codegen/pkg/flagutil"
	"github.com/kralicky/protoconfig/reactive"
	"github.com/kralicky/protoconfig/server"
	"github.com/kralicky/protoconfig/storage"
	"github.com/kralicky/protoconfig/storage/inmemory"
	"github.com/kralicky/protoconfig/test/ext"
	"github.com/kralicky/protoconfig/test/testutil"
	"github.com/kralicky/protoconfig/util"
	"github.com/kralicky/protoconfig/util/fieldmask"
	"github.com/kralicky/protoconfig/util/pathreflect"
	"github.com/kralicky/protoconfig/util/protorand"
)

var _ = Describe("Reactive Controller", Label("unit"), Ordered, func() {
	var ctrl *reactive.Controller[*ext.SampleConfiguration]
	var defaultStore, activeStore storage.ValueStoreT[*ext.SampleConfiguration]

	BeforeEach(func() {
		defaultStore = inmemory.NewValueStore[*ext.SampleConfiguration](util.ProtoClone)
		activeStore = inmemory.NewValueStore[*ext.SampleConfiguration](util.ProtoClone)
		ctrl = reactive.NewController(server.NewDefaultingConfigTracker(defaultStore, activeStore, flagutil.LoadDefaults))
		ctx, ca := context.WithCancel(context.Background())
		Expect(ctrl.Start(ctx)).To(Succeed())
		DeferCleanup(ca)
	})

	It("should create reactive messages", func(ctx SpecContext) {
		msg := &ext.SampleConfiguration{}
		rand := protorand.New[*ext.SampleConfiguration]()
		rand.ExcludeMask(&fieldmaskpb.FieldMask{
			Paths: []string{
				"revision",
			},
		})
		rand.Seed(GinkgoRandomSeed())

		By("creating reactive messages for every possible path")
		allPaths := pathreflect.AllPaths(msg)
		reactiveMsgs := make([]reactive.Value, len(allPaths))

		verifyWatches := func(spec *ext.SampleConfiguration, ws []<-chan protoreflect.Value, pathsToCheck ...map[string]struct{}) {
			recvFailures := []error{}
		ALL_PATHS:
			for i := 0; i < len(allPaths); i++ {
				path := allPaths[i]
				rm := reactiveMsgs[i]
				w := ws[i]

				if strings.HasPrefix(path.String(), "(ext.SampleConfiguration).revision") {
					// ignore the revision field; a reactive message for it has undefined behavior
					select {
					case <-w:
					default:
					}
					continue
				}

				if len(pathsToCheck) > 0 {
					if _, ok := pathsToCheck[0][path[1:].String()[1:]]; !ok {
						Expect(w).NotTo(Receive(), "expected not to receive an update for path %s", path)
						continue
					}
				}

				var v protoreflect.Value
			RECV:
				for i := 0; i < 10; i++ {
					select {
					case v = <-w:
						break RECV
					default:
						time.Sleep(10 * time.Millisecond)
					}
					if i == 9 {
						recvFailures = append(recvFailures, errors.New("did not receive an update for path "+path.String()))
						continue ALL_PATHS
					}
				}
				var actual protoreflect.Value
				if spec == nil {
					actual = protoreflect.ValueOf(nil)
				} else {
					actual = pathreflect.Value(spec, path)
				}
				Expect(v).To(testutil.ProtoValueEqual(rm.Value()))
				Expect(v).To(testutil.ProtoValueEqual(actual))
			}

			Expect(errors.Join(recvFailures...)).To(BeNil())

			for i, c := range ws {
				if len(c) > 0 {
					Expect(c).To(HaveLen(0),
						fmt.Sprintf("expected all watchers to be read; channel for key %s has %d unread elements", allPaths[i].String(), len(c)))
				}
			}
		}

		watches := make([]<-chan protoreflect.Value, len(allPaths))
		for i, path := range allPaths {
			rm := ctrl.Reactive(path)
			reactiveMsgs[i] = rm

			c := rm.Watch(ctx)
			watches[i] = c

			Expect(len(c)).To(BeZero())
		}

		By("setting all fields in the spec to random values")
		spec := rand.MustGen()
		err := activeStore.Put(ctx, spec)
		Expect(err).NotTo(HaveOccurred())

		By("verifying that all reactive messages received an update")
		verifyWatches(spec, watches)

		By("adding a second watch to each reactive message")
		watches2 := make([]<-chan protoreflect.Value, len(watches))

		for i, rm := range reactiveMsgs {
			watches2[i] = rm.Watch(ctx)
		}
		ctrl.DebugDumpReactiveMessagesInfo(GinkgoWriter)

		By("verifying that new watches receive the current value")
		verifyWatches(spec, watches2)

		By("modifying all fields in the spec")
		spec2 := rand.MustGen()
		err = activeStore.Put(ctx, spec2)
		Expect(err).NotTo(HaveOccurred())

		By("verifying that both watches received an update")
		// some fields have a limited set of possible values
		updatedFields := fieldmask.Diff(spec.ProtoReflect(), spec2.ProtoReflect()).Paths
		pathsToCheck := map[string]struct{}{}
		for _, path := range updatedFields {
			parts := strings.Split(path, ".")
			for i := range parts {
				pathsToCheck[strings.Join(parts[:i+1], ".")] = struct{}{}
			}
		}
		verifyWatches(spec2, watches2, pathsToCheck)
		verifyWatches(spec2, watches, pathsToCheck)

		By("deleting the configuration")
		err = activeStore.Delete(ctx)
		Expect(err).NotTo(HaveOccurred())

		By("verifying that all reactive messages received an update")
		verifyWatches(nil, watches2)
		verifyWatches(nil, watches)
	})

	When("a reactive message is watched before a value is set", func() {
		It("should receive the value when it is set", func(ctx SpecContext) {
			msg := &ext.SampleConfiguration{}
			rm1 := ctrl.Reactive(msg.ProtoPath().StringField())
			rm2 := ctrl.Reactive(msg.ProtoPath().MessageField().Field5().Field3())
			w1 := rm1.Watch(ctx)
			w2 := rm2.Watch(ctx)
			w2_2 := rm2.Watch(ctx)
			Expect(len(w1)).To(BeZero())
			Expect(len(w2)).To(BeZero())
			Expect(len(w2_2)).To(BeZero())

			spec := &ext.SampleConfiguration{
				StringField: lo.ToPtr("foo"),
				MessageField: &ext.SampleMessage{
					Field5: &ext.Sample5FieldMsg{
						Field3: 1234,
					},
				},
			}
			err := activeStore.Put(ctx, spec)
			Expect(err).NotTo(HaveOccurred())

			var v protoreflect.Value
			Eventually(w1).Should(Receive(&v))
			Expect(v).To(testutil.ProtoValueEqual(protoreflect.ValueOfString("foo")))

			Eventually(w2).Should(Receive(&v))
			Expect(v).To(testutil.ProtoValueEqual(protoreflect.ValueOfInt32(1234)))
			Eventually(w2_2).Should(Receive(&v))
			Expect(v).To(testutil.ProtoValueEqual(protoreflect.ValueOfInt32(1234)))
		})
	})

	When("a reactive message is watched after a value is set", func() {
		It("should receive the value immediately", func(ctx SpecContext) {
			spec := &ext.SampleConfiguration{
				StringField: lo.ToPtr("foo"),
			}
			err := activeStore.Put(ctx, spec)
			Expect(err).NotTo(HaveOccurred())

			msg := &ext.SampleConfiguration{}
			rm := ctrl.Reactive(msg.ProtoPath().StringField())
			w := rm.Watch(ctx)

			var v protoreflect.Value
			Eventually(w).Should(Receive(&v))
			Expect(v).To(testutil.ProtoValueEqual(protoreflect.ValueOfString("foo")))
		})
	})

	When("the active store has an existing value on controller creation", func() {
		It("should start with the existing revision and value", func() {
			spec := &ext.SampleConfiguration{
				StringField: lo.ToPtr("foo"),
			}
			defaultStore := inmemory.NewValueStore[*ext.SampleConfiguration](util.ProtoClone)
			activeStore := inmemory.NewValueStore[*ext.SampleConfiguration](util.ProtoClone)

			err := activeStore.Put(context.Background(), spec)
			Expect(err).NotTo(HaveOccurred())

			ctrl = reactive.NewController(server.NewDefaultingConfigTracker(defaultStore, activeStore, flagutil.LoadDefaults))
			ctx, ca := context.WithCancel(context.Background())
			Expect(ctrl.Start(ctx)).To(Succeed())
			DeferCleanup(ca)

			rm := ctrl.Reactive(spec.ProtoPath().StringField())
			w := rm.Watch(ctx)

			var v protoreflect.Value
			Eventually(w).Should(Receive(&v))
			Expect(v).To(testutil.ProtoValueEqual(protoreflect.ValueOfString("foo")))
		})
	})

	When("creating multiple reactive messages for the same path", func() {
		It("should duplicate all updates", func(ctx SpecContext) {
			msg := &ext.SampleConfiguration{}
			rm1 := ctrl.Reactive(msg.ProtoPath().StringField())
			rm2 := ctrl.Reactive(msg.ProtoPath().StringField())
			Expect(rm1).To(Equal(rm2)) // the underlying reactive value should be the same

			w1 := rm1.Watch(ctx)
			w2 := rm2.Watch(ctx)

			spec := &ext.SampleConfiguration{
				StringField: lo.ToPtr("foo"),
			}
			err := activeStore.Put(ctx, spec)
			Expect(err).NotTo(HaveOccurred())

			var v1, v2 protoreflect.Value
			Eventually(w1).Should(Receive(&v1))
			Eventually(w2).Should(Receive(&v2))
			Expect(v1).To(testutil.ProtoValueEqual(protoreflect.ValueOfString("foo")))
			Expect(v2).To(testutil.ProtoValueEqual(protoreflect.ValueOfString("foo")))
		})
	})

	When("a value is changed", func() {
		When("it only has watchers on parent paths", func() {
			It("should update the parent reactive message", func(ctx SpecContext) {
				msg := &ext.SampleConfiguration{}
				rm := ctrl.Reactive(protopath.Path(msg.ProtoPath().MessageField()))
				w := rm.Watch(ctx)

				spec := &ext.SampleConfiguration{
					MessageField: &ext.SampleMessage{
						Field1: &ext.Sample1FieldMsg{
							Field1: 1234,
						},
					},
				}
				err := activeStore.Put(ctx, util.ProtoClone(spec))
				Expect(err).NotTo(HaveOccurred())

				var v protoreflect.Value
				Eventually(w).Should(Receive(&v))
				Expect(v).To(testutil.ProtoValueEqual(protoreflect.ValueOfMessage((&ext.SampleMessage{
					Field1: &ext.Sample1FieldMsg{
						Field1: 1234,
					},
				}).ProtoReflect())))
			})
		})
	})
})
