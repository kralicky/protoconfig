package conformance_server

import (
	"context"
	"fmt"
	"io"
	mathrand "math/rand"
	"sort"
	"sync"
	"unsafe"

	"github.com/kralicky/protoconfig/server"
	"github.com/kralicky/protoconfig/storage"
	"github.com/kralicky/protoconfig/test/testutil"
	"github.com/kralicky/protoconfig/util"
	"github.com/kralicky/protoconfig/util/fieldmask"
	"github.com/kralicky/protoconfig/util/merge"
	"github.com/kralicky/protoconfig/util/protorand"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func DefaultingConfigTrackerTestSuite[
	T server.ConfigType[T],
](newDefaultStore, newActiveStore func() storage.ValueStoreT[T]) func() {
	return func() {
		transform := func(v storage.WatchEvent[storage.KeyRevision[T]]) T {
			if lo.IsEmpty(v.Current) {
				return lo.Empty[T]()
			}
			return v.Current.Value()
		}
		withRevision := func(t T, rev int64) T {
			server.SetRevision(t, rev)
			return t
		}
		redacted := func(t T) T {
			t.RedactSecrets()
			return t
		}
		withoutRevision := func(t T) T {
			server.UnsetRevision(t)
			return t
		}

		var (
			wctx          context.Context
			wca           context.CancelFunc
			configTracker *server.DefaultingConfigTracker[T]
		)

		rand := protorand.New[T]()
		rand.ExcludeMask(&fieldmaskpb.FieldMask{
			Paths: []string{
				"revision",
			},
		})
		rand.Seed(GinkgoRandomSeed())
		mustGen := func() T {
			t := rand.MustGen()
			server.UnsetRevision(t)
			return t
		}
		mustGenPartial := func(p float64) T {
			t := rand.MustGenPartial(p)
			server.UnsetRevision(t)
			return t
		}
		var setDefaults func(T)
		var newDefaults, newDefaultsRedacted func() T
		{
			defaults := mustGen()
			defaultsRedacted := util.ProtoClone(defaults)
			defaultsRedacted.RedactSecrets()
			setDefaults = func(t T) {
				merge.MergeWithReplace(t, defaults)
			}
			newDefaults = func() T {
				return util.ProtoClone(defaults)
			}
			newDefaultsRedacted = func() T {
				return util.ProtoClone(defaultsRedacted)
			}
		}

		Context("Common Actions", func() {
			var defaultStore, activeStore storage.ValueStoreT[T]
			var updateC <-chan storage.WatchEvent[storage.KeyRevision[T]]
			BeforeEach(func() {
				wctx, wca = context.WithCancel(context.Background())
				DeferCleanup(wca)
				defaultStore = newDefaultStore()
				activeStore = newActiveStore()
				var err error
				updateC, err = activeStore.Watch(wctx)
				Expect(err).NotTo(HaveOccurred())
				configTracker = server.NewDefaultingConfigTracker(defaultStore, activeStore, setDefaults)
			})
			When("getting the default config", func() {
				It("should return a default config if it is in the store", func() {
					expected := mustGen()
					Expect(configTracker.SetDefault(wctx, expected)).To(Succeed())

					conf, err := configTracker.GetDefault(wctx)
					Expect(err).NotTo(HaveOccurred())

					expected.RedactSecrets()
					server.CopyRevision(expected, conf)
					Expect(conf).To(testutil.ProtoEqual(expected))
				})

				It("should return a new default config if it is not found in the store", func() {
					conf, err := configTracker.GetDefault(wctx)
					Expect(err).NotTo(HaveOccurred())

					Expect(conf).To(testutil.ProtoEqual(withRevision(newDefaultsRedacted(), 0)))
				})
			})

			When("setting the default config", func() {
				Specify("subsequent calls to GetDefaultConfig should return the new default", func() {
					newDefault := mustGen()

					err := configTracker.SetDefault(wctx, newDefault)
					Expect(err).NotTo(HaveOccurred())

					conf, err := configTracker.GetDefault(wctx)
					Expect(err).NotTo(HaveOccurred())

					newDefault.RedactSecrets()
					server.CopyRevision(newDefault, conf)
					Expect(conf).To(testutil.ProtoEqual(newDefault))
				})
				When("applying configurations with secrets", func() {
					It("should correctly redact secrets", func() {
						newDefault := mustGen()
						err := configTracker.SetDefault(wctx, newDefault)
						Expect(err).NotTo(HaveOccurred())

						conf, err := configTracker.GetDefault(wctx)
						Expect(err).NotTo(HaveOccurred())
						Expect(conf).NotTo(testutil.ProtoEqual(newDefault))

						newDefault.RedactSecrets()
						server.CopyRevision(newDefault, conf)
						Expect(conf).To(testutil.ProtoEqual(newDefault))
					})
				})
			})

			When("getting the active config", func() {
				When("there is an active config in the store", func() {
					Specify("GetConfig should return the active config", func() {
						active := mustGen()
						Expect(configTracker.Apply(wctx, active)).To(Succeed())

						defaults := newDefaults()
						merge.MergeWithReplace(defaults, active)

						Eventually(updateC).Should(Receive(WithTransform(transform, testutil.ProtoEqual(defaults))))

						conf, err := configTracker.Get(wctx)
						Expect(err).NotTo(HaveOccurred())
						defaults.RedactSecrets()
						server.CopyRevision(defaults, conf)
						Expect(conf).To(testutil.ProtoEqual(defaults))
					})
					Specify("GetConfigOrDefault should return the active config", func() {
						expected := mustGen()
						Expect(configTracker.Apply(wctx, expected)).To(Succeed())

						defaults := newDefaults()
						merge.MergeWithReplace(defaults, expected)

						Eventually(updateC).Should(Receive(WithTransform(transform, testutil.ProtoEqual(defaults))))

						conf, err := configTracker.GetActiveOrDefault(wctx)
						Expect(err).NotTo(HaveOccurred())
						defaults.RedactSecrets()
						server.CopyRevision(defaults, conf)
						Expect(conf).To(testutil.ProtoEqual(defaults))
					})
				})
				When("there is no active config in the store", func() {
					Specify("GetConfig should return an error", func() {
						conf, err := configTracker.Get(wctx)
						Expect(storage.IsNotFound(err)).To(BeTrue())
						Expect(conf).To(BeNil())
					})
					Specify("GetConfigOrDefault should return a default config", func() {
						defaultConfig := newDefaults()
						Expect(configTracker.SetDefault(wctx, defaultConfig)).To(Succeed())
						conf, err := configTracker.GetActiveOrDefault(wctx)

						Expect(err).NotTo(HaveOccurred())
						defaultConfig.RedactSecrets()
						server.CopyRevision(defaultConfig, conf)
						Expect(conf).To(testutil.ProtoEqual(defaultConfig))
					})
				})
				When("an error occurs looking up the active config", func() {
					It("should return the error", func() {
						_, err := configTracker.Get(wctx)
						Expect(storage.IsNotFound(err)).To(BeTrue())
					})
				})
			})

			When("applying the active config", func() {
				When("there is no existing active config in the store", func() {
					It("should merge the incoming config with the defaults", func() {
						newActive := mustGenPartial(0.25)
						defaultConfig := newDefaults()
						Expect(configTracker.SetDefault(wctx, defaultConfig)).To(Succeed())

						mergedConfig := defaultConfig
						merge.MergeWithReplace(mergedConfig, newActive)

						err := configTracker.Apply(wctx, newActive)
						Expect(err).NotTo(HaveOccurred())
						Eventually(updateC).Should(Receive(WithTransform(transform, testutil.ProtoEqual(withoutRevision(mergedConfig)))))

						activeConfig, err := configTracker.Get(wctx)
						Expect(err).NotTo(HaveOccurred())
						mergedConfig.RedactSecrets()
						server.CopyRevision(mergedConfig, activeConfig)

						Expect(activeConfig).To(testutil.ProtoEqual(mergedConfig))
					})
					When("there is no default config in the store", func() {
						It("should merge the incoming config with new defaults", func() {
							newActive := mustGenPartial(0.25)

							err := configTracker.Apply(wctx, newActive)
							Expect(err).NotTo(HaveOccurred())

							newDefaults := newDefaults()
							merge.MergeWithReplace(newDefaults, newActive)

							Eventually(updateC).Should(Receive(WithTransform(transform, testutil.ProtoEqual(newDefaults))))

							activeConfig, err := configTracker.Get(wctx)
							Expect(err).NotTo(HaveOccurred())

							newDefaults.RedactSecrets()
							server.CopyRevision(newDefaults, activeConfig)
							Expect(activeConfig).To(testutil.ProtoEqual(newDefaults))
						})
					})
					When("applying with redacted placeholders", func() {
						It("should preserve the underlying secret value", func() {
							defaults := newDefaults()
							Expect(configTracker.SetDefault(wctx, defaults)).To(Succeed())

							newActive := withRevision(mustGen(), 0)
							// redact secrets before applying, which sets them to *** preserving the underlying value
							newActive.RedactSecrets()
							Expect(configTracker.Apply(wctx, newActive)).To(Succeed())
							var event storage.WatchEvent[storage.KeyRevision[T]]
							Eventually(updateC).Should(Receive(&event))

							// redact the defaults, then unredact them using the active config.
							// if the underlying secret was preserved, this should correctly
							// restore the secret fields in the original defaults.
							clonedDefaults := util.ProtoClone(defaults)
							clonedDefaults.RedactSecrets()
							clonedDefaults.UnredactSecrets(newActive)
							Expect(defaults).To(testutil.ProtoEqual(clonedDefaults))
						})
					})
				})
				When("there is an existing active config in the store", func() {
					It("should merge with the existing active config", func() {
						oldActive := mustGen()

						newActive := mustGenPartial(0.5)
						Expect(configTracker.Apply(wctx, oldActive)).To(Succeed())
						Eventually(updateC).Should(Receive(WithTransform(transform, testutil.ProtoEqual(oldActive))))
						mergedConfig := oldActive
						merge.MergeWithReplace(mergedConfig, newActive)

						server.CopyRevision(newActive, mergedConfig)
						err := configTracker.Apply(wctx, newActive)
						Expect(err).NotTo(HaveOccurred())

						Eventually(updateC).Should(Receive(WithTransform(transform, testutil.ProtoEqual(withoutRevision(mergedConfig)))))

						activeConfig, err := configTracker.Get(wctx)
						Expect(err).NotTo(HaveOccurred())
						mergedConfig.RedactSecrets()
						server.CopyRevision(mergedConfig, activeConfig)

						Expect(activeConfig).To(testutil.ProtoEqual(mergedConfig))
					})
				})
			})
			When("setting the active config", func() {
				It("should ignore any existing active config and merge with the default", func() {
					def := newDefaults()
					Expect(configTracker.SetDefault(wctx, def)).To(Succeed())

					defClone := util.ProtoClone(def)

					updates := mustGenPartial(0.1)

					merge.MergeWithReplace(defClone, updates)

					err := configTracker.Apply(wctx, updates)
					Expect(err).NotTo(HaveOccurred())
					Eventually(updateC).Should(Receive(WithTransform(transform, testutil.ProtoEqual(defClone))))

					activeConfig, err := configTracker.Get(wctx)
					Expect(err).NotTo(HaveOccurred())
					defClone.RedactSecrets()
					server.CopyRevision(defClone, activeConfig)

					Expect(activeConfig).To(testutil.ProtoEqual(defClone))
				})
			})
			When("resetting the active config", func() {
				It("should delete the config from the underlying store", func() {
					updates := mustGenPartial(0.1)

					Expect(configTracker.Apply(wctx, updates)).To(Succeed())
					newActive := newDefaults()
					merge.MergeWithReplace(newActive, updates)
					Eventually(updateC).Should(Receive(WithTransform(transform, testutil.ProtoEqual(newActive))))

					err := configTracker.Reset(wctx, nil, lo.Empty[T]())
					Expect(err).NotTo(HaveOccurred())

					Eventually(updateC).Should(Receive(WithTransform(transform, BeNil())))

					_, err = configTracker.Get(wctx)
					Expect(err).To(testutil.MatchStatusCode(storage.ErrNotFound))
				})
				When("an error occurs deleting the config", func() {
					It("should return the error", func() {
						err := configTracker.Reset(wctx, nil, lo.Empty[T]())
						Expect(err).To(testutil.MatchStatusCode(storage.ErrNotFound))
						Expect(updateC).NotTo(Receive())
					})
				})
				When("providing a field mask", func() {
					It("should preserve the fields in the mask", func() {
						conf := mustGen()
						Expect(configTracker.Apply(wctx, conf)).To(Succeed())
						Eventually(updateC).Should(Receive(WithTransform(transform, testutil.ProtoEqual(conf))))

						// generate a random mask
						mask := fieldmask.ByPresence(mustGenPartial(0.25).ProtoReflect())
						Expect(mask.IsValid(conf)).To(BeTrue(), mask.GetPaths())

						err := configTracker.Reset(wctx, mask, lo.Empty[T]())
						Expect(err).NotTo(HaveOccurred())

						expected := newDefaults()
						fieldmask.ExclusiveKeep(conf, mask)
						merge.MergeWithReplace(expected, conf)

						Eventually(updateC).Should(Receive(WithTransform(transform, testutil.ProtoEqual(expected))))
					})
				})
			})
			When("resetting the default config", func() {
				It("should delete the config from the underlying store", func() {
					originalDefault, err := configTracker.GetDefault(wctx)
					Expect(err).NotTo(HaveOccurred())

					modifiedDefault := mustGenPartial(0.5)
					Expect(configTracker.SetDefault(wctx, modifiedDefault)).To(Succeed())

					err = configTracker.ResetDefault(wctx)
					Expect(err).NotTo(HaveOccurred())
					Expect(updateC).NotTo(Receive())

					conf, err := configTracker.GetDefault(wctx)
					Expect(err).NotTo(HaveOccurred())
					Expect(conf).To(Equal(originalDefault))
				})
				When("an error occurs deleting the config", func() {
					It("should return the error", func() {
						err := configTracker.ResetDefault(wctx)
						Expect(storage.IsNotFound(err)).To(BeTrue())
						Expect(updateC).NotTo(Receive())
					})
				})
			})
			When("using dry-run mode", func() {
				When("setting the default config", func() {
					It("should report changes without persisting them", func() {
						newDefault := mustGen()

						results, err := configTracker.DryRunSetDefault(wctx, newDefault)
						Expect(err).NotTo(HaveOccurred())
						Expect(withoutRevision(results.Current)).To(testutil.ProtoEqual(newDefaultsRedacted()))
						conf := results.Modified

						newDefault.RedactSecrets()
						server.CopyRevision(newDefault, conf)
						Expect(conf).To(testutil.ProtoEqual(newDefault))

						conf, err = configTracker.GetDefault(wctx)
						Expect(err).NotTo(HaveOccurred())
						Expect(conf).To(testutil.ProtoEqual(withRevision(newDefaultsRedacted(), 0)))
					})
				})
				When("applying the active config", func() {
					It("should report changes without persisting them", func() {
						newActive := mustGen()

						results, err := configTracker.DryRunApply(wctx, newActive)
						Expect(err).NotTo(HaveOccurred())
						Expect(withoutRevision(results.Current)).To(testutil.ProtoEqual(withoutRevision(newDefaultsRedacted())))
						conf := results.Modified

						newActive.RedactSecrets()
						server.CopyRevision(newActive, conf)
						Expect(conf).To(testutil.ProtoEqual(newActive))

						_, err = configTracker.Get(wctx)
						Expect(err).To(testutil.MatchStatusCode(storage.ErrNotFound))
					})
				})
				When("resetting the default config", func() {
					It("should report changes without persisting them", func() {
						conf := mustGen()
						Expect(configTracker.SetDefault(wctx, conf)).To(Succeed())
						conf.RedactSecrets()

						results, err := configTracker.DryRunResetDefault(wctx)
						Expect(err).NotTo(HaveOccurred())

						Expect(withoutRevision(results.Current)).To(testutil.ProtoEqual(withoutRevision(conf)))
						Expect(results.Modified).To(testutil.ProtoEqual(withoutRevision(newDefaultsRedacted())))

						conf, err = configTracker.GetDefault(wctx)
						Expect(err).NotTo(HaveOccurred())
						Expect(conf).To(testutil.ProtoEqual(withoutRevision(conf)))
					})
				})
				When("resetting the active config", func() {
					When("neither mask nor patch are provided", func() {
						It("should report changes without persisting them", func() {
							conf := mustGen()
							Expect(configTracker.Apply(wctx, conf)).To(Succeed())
							conf.RedactSecrets()

							results, err := configTracker.DryRunReset(wctx, nil, lo.Empty[T]())
							Expect(err).NotTo(HaveOccurred())

							Expect(withoutRevision(results.Current)).To(testutil.ProtoEqual(withoutRevision(conf)))
							Expect(results.Modified).To(testutil.ProtoEqual(withoutRevision(newDefaultsRedacted())))

							conf, err = configTracker.Get(wctx)
							Expect(err).NotTo(HaveOccurred())
							Expect(conf).To(testutil.ProtoEqual(withoutRevision(conf)))
						})
					})
					When("a mask is provided, but no patch", func() {
					})
					When("both a mask and patch are provided", func() {
					})
				})
			})
			When("querying history", func() {
				When("values are requested", func() {
					It("should redact secrets", func() {
						cfg1 := mustGen()
						Expect(configTracker.SetDefault(wctx, cfg1)).To(Succeed())
						cfg1WithRev, err := configTracker.GetDefault(wctx)
						Expect(err).NotTo(HaveOccurred())
						cfg2 := mustGen()
						cfg2WithRev := util.ProtoClone(cfg2)
						server.CopyRevision(cfg2WithRev, cfg1WithRev)
						Expect(configTracker.SetDefault(wctx, cfg2WithRev)).To(Succeed())
						Expect(configTracker.Apply(wctx, cfg1)).To(Succeed())
						Expect(configTracker.Apply(wctx, cfg2)).To(Succeed())

						historyDefault, err := configTracker.History(wctx, server.Target_Default, storage.IncludeValues(true))
						Expect(err).NotTo(HaveOccurred())
						historyActive, err := configTracker.History(wctx, server.Target_Active, storage.IncludeValues(true))
						Expect(err).NotTo(HaveOccurred())
						Expect(historyDefault).To(HaveLen(2))
						Expect(historyDefault[0].Value()).NotTo(testutil.ProtoEqual(cfg1))
						Expect(historyDefault[1].Value()).NotTo(testutil.ProtoEqual(cfg2))
						Expect(historyActive).To(HaveLen(2))
						Expect(historyActive[0].Value()).NotTo(testutil.ProtoEqual(cfg1))
						Expect(historyActive[1].Value()).NotTo(testutil.ProtoEqual(cfg2))

						cfg1.RedactSecrets()
						cfg2.RedactSecrets()

						Expect(historyDefault[0].Value()).To(testutil.ProtoEqual(cfg1))
						Expect(historyDefault[1].Value()).To(testutil.ProtoEqual(cfg2))
						Expect(historyActive[0].Value()).To(testutil.ProtoEqual(cfg1))
						Expect(historyActive[1].Value()).To(testutil.ProtoEqual(cfg2))
					})
				})
			})
		})

		Context("Watch Stress Testing", func() {
			var numDefaultWatchEvents, numActiveWatchEvents int
			var watchDefault, watchActive <-chan storage.WatchEvent[storage.KeyRevision[T]]

			debugLogger := io.Discard
			var _setDefault func(SpecContext, T)
			setDefault := func(ctx SpecContext) {
				_setDefault(ctx, mustGen())
			}
			_setDefault = func(ctx SpecContext, newDefault T) {
				fmt.Fprintln(debugLogger, "action: setDefault")
				defer func() { fmt.Fprintln(debugLogger, "action: setDefault (end)") }()

				currentDefaultRev := testutil.Must(configTracker.GetDefault(ctx)).GetRevision().GetRevision()
				Expect(configTracker.SetDefault(ctx, withRevision(newDefault, currentDefaultRev))).To(Succeed())
				newDefault.RedactSecrets()
				newDefaultRev := testutil.Must(configTracker.GetDefault(ctx)).GetRevision().GetRevision()
				select {
				case e := <-watchDefault:
					Expect(e.EventType).To(Equal(storage.WatchEventPut))
					Expect(withRevision(redacted(e.Current.Value()), e.Current.Revision())).To(testutil.ProtoEqual(withRevision(newDefault, newDefaultRev)))
				case <-ctx.Done():
					Fail(fmt.Sprintf("timed out waiting for default config: %v", ctx.Err()))
				}
				numDefaultWatchEvents++
			}
			resetDefault := func(ctx SpecContext) {
				fmt.Fprintln(debugLogger, "action: resetDefault")
				defer func() { fmt.Fprintln(debugLogger, "action: resetDefault (end)") }()
				lastDefault, err := configTracker.GetDefault(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(configTracker.ResetDefault(ctx)).To(Succeed())
				select {
				case e := <-watchDefault:
					Expect(e.EventType).To(Equal(storage.WatchEventDelete))
					Expect(withRevision(redacted(e.Previous.Value()), e.Previous.Revision())).To(testutil.ProtoEqual(lastDefault))
				case <-ctx.Done():
					Fail(fmt.Sprintf("timed out waiting for default config: %v", ctx.Err()))
				}
				numDefaultWatchEvents++
			}
			var _setActive func(SpecContext, T)
			setActive := func(ctx SpecContext) {
				_setActive(ctx, mustGen())
			}
			_setActive = func(ctx SpecContext, newActive T) {
				fmt.Fprintln(debugLogger, "action: setActive")
				defer func() { fmt.Fprintln(debugLogger, "action: setActive (end)") }()
				for i := 0; ; i++ {
					currentActive, err := configTracker.Get(ctx)
					if storage.IsNotFound(err) {
						err := configTracker.Apply(ctx, newActive)
						if err != nil {
							if storage.IsConflict(err) {
								continue
							}
							Fail(err.Error())
						}
					} else if err != nil {
						Fail(err.Error())
					} else {
						Expect(configTracker.Apply(ctx, withRevision(newActive, currentActive.GetRevision().GetRevision()))).To(Succeed())
					}
					break
				}
				actualActive, err := configTracker.Get(ctx)
				Expect(err).NotTo(HaveOccurred())
				select {
				case e := <-watchActive:
					Expect(e.EventType).To(Equal(storage.WatchEventPut))
					Expect(withRevision(redacted(e.Current.Value()), e.Current.Revision())).To(testutil.ProtoEqual(actualActive))
				case <-ctx.Done():
					Fail(fmt.Sprintf("timed out waiting for active config: %v", ctx.Err()))
				}
				numActiveWatchEvents++
			}
			var _resetActive func(SpecContext)
			resetActive := func(ctx SpecContext) {
				_resetActive(ctx)
			}
			_resetActive = func(ctx SpecContext) {
				fmt.Fprintln(debugLogger, "action: resetActive")
				defer func() { fmt.Fprintln(debugLogger, "action: resetActive (end)") }()

				lastActive, err := configTracker.Get(ctx)
				Expect(err).NotTo(HaveOccurred())

				Expect(configTracker.Reset(ctx, nil, lo.Empty[T]())).To(Succeed())

				select {
				case e := <-watchActive:
					Expect(e.EventType).To(Equal(storage.WatchEventDelete))
					Expect(e.Current).To(BeNil())
					Expect(withRevision(redacted(e.Previous.Value()), e.Previous.Revision())).To(testutil.ProtoEqual(lastActive))

				case <-ctx.Done():
					Fail(fmt.Sprintf("timed out waiting for active config: %v", ctx.Err()))
				}

				numActiveWatchEvents++
			}
			done := func(ctx SpecContext) {
				fmt.Fprintln(debugLogger, "action: done")
			}
			// state tracker for which actions can be performed at the current time
			actionsEnabled := map[*func(SpecContext)]bool{
				&setDefault:   true,
				&resetDefault: false,
				&setActive:    true,
				&resetActive:  false,
				&done:         false,
			}
			// a table of actions that may become enabled or disabled when each action runs
			actionSideEffects := map[*func(SpecContext)]map[*func(SpecContext)]bool{
				&setDefault: {
					&resetDefault: true,
				},
				&resetDefault: {
					&resetDefault: false,
				},
				&setActive: {
					&resetActive: true,
				},
				&resetActive: {
					&resetActive: false,
				},
				&done: {
					// disable everything
					&setDefault:   false,
					&resetDefault: false,
					&setActive:    false,
					&resetActive:  false,
					&done:         false,
				},
			}
			var defaultStore, activeStore storage.ValueStoreT[T]
			BeforeEach(func() {
				defaultStore = newDefaultStore()
				activeStore = newActiveStore()

				configTracker = server.NewDefaultingConfigTracker(defaultStore, activeStore, setDefaults)

				GinkgoHelper()
				numDefaultWatchEvents = 0
				numActiveWatchEvents = 0
				wctx, wca := context.WithCancel(context.Background())
				var err error
				watchActive, err = activeStore.Watch(wctx)
				Expect(err).NotTo(HaveOccurred())
				watchDefault, err = defaultStore.Watch(wctx)
				Expect(err).NotTo(HaveOccurred())

				watchActive2, err := activeStore.Watch(wctx)
				Expect(err).NotTo(HaveOccurred())
				watchDefault2, err := defaultStore.Watch(wctx)
				Expect(err).NotTo(HaveOccurred())
				actualNumDefaultWatchEvents := 0
				actualNumActiveWatchEvents := 0
				var watches sync.WaitGroup
				watches.Add(2)
				go func() {
					defer watches.Done()
					for range watchDefault2 {
						actualNumDefaultWatchEvents++
					}
				}()
				go func() {
					defer watches.Done()
					for range watchActive2 {
						actualNumActiveWatchEvents++
					}
				}()
				DeferCleanup(func() {
					wca()
					watches.Wait()
					Expect(watchDefault).To(HaveLen(0))
					Expect(watchActive).To(HaveLen(0))
					Expect(actualNumDefaultWatchEvents).To(Equal(numDefaultWatchEvents), "incorrect number of watch events received from default store")
					Expect(actualNumActiveWatchEvents).To(Equal(numActiveWatchEvents), "incorrect number of watch events received from active store")
				})
			})
			random := mathrand.New(mathrand.NewSource(GinkgoRandomSeed()))
			for i := 0; i < 10; i++ {
				Specify(fmt.Sprintf("random actions (%d/10)", i+1), func(ctx SpecContext) {
					minActionsBeforeCancel := 100
					for actionCount := 0; ; actionCount++ {
						// select an action from the list of enabled actions
						enabled := []*func(SpecContext){}
						for action, isEnabled := range actionsEnabled {
							if isEnabled {
								enabled = append(enabled, action)
							}
						}
						if len(enabled) == 0 {
							// no possible actions left
							break
						}
						sort.Slice(enabled, func(i, j int) bool {
							return uintptr(unsafe.Pointer(enabled[i])) < uintptr(unsafe.Pointer(enabled[j]))
						})
						selectedAction := enabled[random.Int()%len(enabled)]
						(*selectedAction)(ctx)

						// update the list of enabled actions based on the side effects of the action that was just run
						for action, enabled := range actionSideEffects[selectedAction] {
							actionsEnabled[action] = enabled
						}
						if actionCount == minActionsBeforeCancel {
							actionsEnabled[&done] = true
						}
					}
				})
			}
		})
	}
}
