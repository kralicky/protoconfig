package server_test

import (
	"context"

	corev1 "github.com/kralicky/protoconfig/apis/core/v1"
	"github.com/kralicky/protoconfig/server"
	"github.com/kralicky/protoconfig/test/ext"
	"github.com/kralicky/protoconfig/test/testutil"
	"github.com/kralicky/protoconfig/util"
	"github.com/kralicky/protoconfig/util/merge"
	"github.com/kralicky/protoconfig/util/protorand"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

var _ = Describe("Base Config Server", Label("unit"), Ordered, func() {
	var cs *server.BaseConfigServer[
		*server.GetRequest,
		*ext.SampleSetRequest,
		*ext.SampleResetRequest,
		*server.ConfigurationHistoryRequest,
		*ext.SampleConfigurationHistoryResponse,
		*ext.SampleConfiguration,
	]
	rand := protorand.New[*ext.SampleConfiguration]()
	rand.ExcludeMask(&fieldmaskpb.FieldMask{
		Paths: []string{
			"revision",
			"enabled",
		},
	})
	rand.Seed(GinkgoRandomSeed())
	mustGen := func() *ext.SampleConfiguration {
		t := rand.MustGen()
		server.UnsetRevision(t)
		return t
	}
	var setDefaults func(*ext.SampleConfiguration)
	var newDefaults func() *ext.SampleConfiguration
	{
		defaults := mustGen()
		setDefaults = func(t *ext.SampleConfiguration) {
			merge.MergeWithReplace(t, defaults)
		}
		newDefaults = func() *ext.SampleConfiguration {
			return util.ProtoClone(defaults)
		}
	}

	BeforeEach(func() {
		cs = cs.Build(newValueStore(), newValueStore(), setDefaults)
	})
	Context("GetConfiguration", func() {
		It("should forward the request to the tracker", func(ctx context.Context) {
			By("generating a random configuration")
			config := rand.MustGen()
			By("setting the active configuration")
			Expect(cs.Tracker().ApplyConfig(ctx, util.ProtoClone(config))).To(Succeed())
			By("getting the configuration")
			res, err := cs.GetConfiguration(ctx, &server.GetRequest{})
			Expect(err).NotTo(HaveOccurred())
			config.RedactSecrets()
			config.Revision = res.Revision

			Expect(res).To(testutil.ProtoEqual(config))
		})
	})

	Context("GetDefaultConfiguration", func() {
		It("should forward the request to the tracker", func(ctx context.Context) {
			By("getting the default configuration")
			defaults := newDefaults()
			res, err := cs.GetDefaultConfiguration(ctx, &server.GetRequest{})
			Expect(err).NotTo(HaveOccurred())
			defaults.RedactSecrets()
			defaults.Revision = res.Revision
			Expect(res).To(testutil.ProtoEqual(defaults))
		})
	})
	Context("Install", func() {
		It("should set the enabled field of the config to true and apply it", func(ctx context.Context) {
			By("generating a random configuration")
			config := mustGen()
			By("setting the active configuration")
			Expect(cs.Tracker().ApplyConfig(ctx, util.ProtoClone(config))).To(Succeed())
			By("installing the configuration")
			_, err := cs.Install(ctx, &emptypb.Empty{})
			Expect(err).NotTo(HaveOccurred())
			By("getting the configuration")
			res, err := cs.GetConfiguration(ctx, &server.GetRequest{})
			Expect(err).NotTo(HaveOccurred())
			config.RedactSecrets()
			config.Revision = res.Revision
			By("checking the enabled field")
			config.Enabled = lo.ToPtr(true)
			Expect(res).To(testutil.ProtoEqual(config))
		})
	})
	Context("Uninstall", func() {
		It("should set the enabled field of the config to false and apply it", func(ctx context.Context) {
			By("generating a random configuration")
			config := mustGen()
			By("setting the active configuration")
			Expect(cs.Tracker().ApplyConfig(ctx, util.ProtoClone(config))).To(Succeed())
			By("uninstalling the configuration")
			_, err := cs.Uninstall(ctx, &emptypb.Empty{})
			Expect(err).NotTo(HaveOccurred())
			By("getting the configuration")
			res, err := cs.GetConfiguration(ctx, &server.GetRequest{})
			Expect(err).NotTo(HaveOccurred())
			config.RedactSecrets()
			config.Revision = res.Revision
			By("checking the enabled field")
			config.Enabled = lo.ToPtr(false)
			Expect(res).To(testutil.ProtoEqual(config))
		})
	})
	Context("ResetConfiguration", func() {
		When("no mask or patch is provided", func() {
			It("should forward the request to the tracker as-is", func(ctx context.Context) {
				By("modifying the active configuration")
				Expect(cs.Tracker().ApplyConfig(ctx, mustGen())).To(Succeed())
				By("resetting the active configuration")
				_, err := cs.ResetConfiguration(ctx, &ext.SampleResetRequest{})
				Expect(err).NotTo(HaveOccurred())
				By("getting the configuration")
				res, err := cs.GetConfiguration(ctx, &server.GetRequest{})
				Expect(err).NotTo(HaveOccurred())
				def := newDefaults()
				def.RedactSecrets()
				def.Revision = res.Revision
				Expect(res).To(testutil.ProtoEqual(def))

				By("checking that history is preserved")
				history, err := cs.ConfigurationHistory(ctx, &server.ConfigurationHistoryRequest{
					Target: server.Target_ActiveConfiguration,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(history.Entries).To(HaveLen(2)) // apply, reset
			})
		})
		When("a mask is provided, and no patch is provided", func() {
			It("should ensure the enabled field is masked", func(ctx context.Context) {
				By("modifying the active configuration")
				active := mustGen()
				Expect(cs.Tracker().ApplyConfig(ctx, util.ProtoClone(active))).To(Succeed())
				By("installing")
				_, err := cs.Install(ctx, &emptypb.Empty{})
				Expect(err).NotTo(HaveOccurred())

				By("resetting the active configuration")
				_, err = cs.ResetConfiguration(ctx, &ext.SampleResetRequest{
					Mask: &fieldmaskpb.FieldMask{
						Paths: []string{
							"messageField.field1",
						},
					},
				})
				Expect(err).NotTo(HaveOccurred())
				By("getting the configuration")
				res, err := cs.GetConfiguration(ctx, &server.GetRequest{})
				Expect(err).NotTo(HaveOccurred())
				expected := newDefaults()
				expected.RedactSecrets()
				expected.Enabled = lo.ToPtr(true)
				expected.MessageField.Field1 = util.ProtoClone(active.MessageField.Field1)
				expected.Revision = res.Revision
				Expect(res).To(testutil.ProtoEqual(expected))

				By("checking that history is preserved")
				history, err := cs.ConfigurationHistory(ctx, &server.ConfigurationHistoryRequest{
					Target: server.Target_ActiveConfiguration,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(history.Entries).To(HaveLen(3)) // apply, install, reset
			})
		})
		When("a mask and patch are provided", func() {
			It("should ensure the enabled field is masked, and the enabled field is cleared from the patch", func(ctx context.Context) {
				By("modifying the active configuration")
				active := mustGen()
				Expect(cs.Tracker().ApplyConfig(ctx, util.ProtoClone(active))).To(Succeed())
				By("installing")
				_, err := cs.Install(ctx, &emptypb.Empty{})
				Expect(err).NotTo(HaveOccurred())

				By("resetting the active configuration")
				_, err = cs.ResetConfiguration(ctx, &ext.SampleResetRequest{
					Mask: &fieldmaskpb.FieldMask{
						Paths: []string{
							"messageField.field1",
							"messageField.field2.field1",
						},
					},
					Patch: &ext.SampleConfiguration{
						Enabled: lo.ToPtr(false),
						MessageField: &ext.SampleMessage{
							Field2: &ext.Sample2FieldMsg{
								Field1: 12345,
							},
						},
					},
				})
				Expect(err).NotTo(HaveOccurred())
				By("getting the configuration")
				res, err := cs.GetConfiguration(ctx, &server.GetRequest{})
				Expect(err).NotTo(HaveOccurred())
				expected := newDefaults()
				expected.RedactSecrets()
				expected.Enabled = lo.ToPtr(true)
				expected.MessageField.Field1 = util.ProtoClone(active.MessageField.Field1)
				expected.MessageField.Field2.Field1 = 12345
				expected.Revision = res.Revision
				Expect(res).To(testutil.ProtoEqual(expected))

				By("checking that history is preserved")
				history, err := cs.ConfigurationHistory(ctx, &server.ConfigurationHistoryRequest{
					Target: server.Target_ActiveConfiguration,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(history.Entries).To(HaveLen(3)) // apply, install, reset
			})
		})
	})
	Context("ResetDefaultConfiguration", func() {
		It("should forward the request to the tracker", func(ctx context.Context) {
			By("modifying the default configuration")
			defaults := newDefaults()
			Expect(cs.Tracker().SetDefaultConfig(ctx, mustGen())).To(Succeed())
			By("resetting the default configuration")
			_, err := cs.ResetDefaultConfiguration(ctx, &emptypb.Empty{})
			Expect(err).NotTo(HaveOccurred())
			By("getting the default configuration")
			res, err := cs.GetDefaultConfiguration(ctx, &server.GetRequest{})
			Expect(err).NotTo(HaveOccurred())
			defaults.RedactSecrets()
			defaults.Revision = res.Revision
			Expect(res).To(testutil.ProtoEqual(defaults))
		})
	})
	Context("SetConfiguration", func() {
		It("should unset the enabled field and forward the request to the tracker", MustPassRepeatedly(100), func(ctx context.Context) {
			By("generating a random configuration")
			config := mustGen()
			config.Enabled = nil
			By("setting the active configuration")
			Expect(cs.Tracker().ApplyConfig(ctx, util.ProtoClone(config))).To(Succeed())
			By("setting the configuration")
			config.Enabled = lo.ToPtr(true)
			_, err := cs.SetConfiguration(ctx, &ext.SampleSetRequest{
				Spec: util.ProtoClone(config),
			})
			Expect(err).NotTo(HaveOccurred())
			By("getting the configuration")
			res, err := cs.GetConfiguration(ctx, &server.GetRequest{})
			Expect(err).NotTo(HaveOccurred())
			By("checking the enabled field")
			Expect(res.Enabled).To(BeNil())
		})
	})
	Context("SetDefaultConfiguration", func() {
		It("should unset the enabled field and forward the request to the tracker", func(ctx context.Context) {
			defaults := newDefaults()
			defaults.Enabled = lo.ToPtr(true)
			By("setting the default configuration")
			_, err := cs.SetDefaultConfiguration(ctx, &ext.SampleSetRequest{
				Spec: defaults,
			})
			Expect(err).NotTo(HaveOccurred())
			By("getting the default configuration")
			res, err := cs.GetDefaultConfiguration(ctx, &server.GetRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Enabled).To(BeNil())
		})
	})
	Context("ConfigurationHistory", func() {
		It("should forward the request to the tracker, and translate the response type", func(ctx context.Context) {
			By("applying multiple configurations")
			configs := make([]*ext.SampleConfiguration, 10)
			for i := 0; i < 10; i++ {
				configs[i] = mustGen()
				Expect(cs.Tracker().ApplyConfig(ctx, configs[i])).To(Succeed())
			}
			By("getting the configuration history without values")
			res, err := cs.ConfigurationHistory(ctx, &server.ConfigurationHistoryRequest{
				IncludeValues: false,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.GetEntries()).To(HaveLen(10))
			for i := 0; i < 10; i++ {
				Expect(res.Entries[i].GetRevision()).NotTo(BeNil())
				clone := util.ProtoClone(res.Entries[i])
				clone.Revision = nil
				Expect(clone).To(testutil.ProtoEqual(&ext.SampleConfiguration{}))
			}

			By("getting the configuration history with values")
			res, err = cs.ConfigurationHistory(ctx, &server.ConfigurationHistoryRequest{
				IncludeValues: true,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.GetEntries()).To(HaveLen(10))
			for i := 0; i < 10; i++ {
				expected := util.ProtoClone(configs[i])
				expected.RedactSecrets()
				expected.Revision = res.GetEntries()[i].GetRevision()
				Expect(res.GetEntries()[i]).To(testutil.ProtoEqual(expected))
			}
		})
	})
})

var _ = Describe("Context Keyable Config Server", Label("unit"), Ordered, func() {
	var cs *server.ContextKeyableConfigServer[
		*ext.SampleGetRequest,
		*ext.SampleSetRequest,
		*ext.SampleResetRequest,
		*ext.SampleHistoryRequest,
		*ext.SampleConfigurationHistoryResponse,
		*ext.SampleConfiguration,
	]
	rand := protorand.New[*ext.SampleConfiguration]()
	rand.ExcludeMask(&fieldmaskpb.FieldMask{
		Paths: []string{
			"revision",
			"enabled",
		},
	})
	rand.Seed(GinkgoRandomSeed())
	mustGen := func() *ext.SampleConfiguration {
		t := rand.MustGen()
		server.UnsetRevision(t)
		return t
	}
	var setDefaults func(*ext.SampleConfiguration)
	var newDefaults func() *ext.SampleConfiguration
	{
		defaults := mustGen()
		setDefaults = func(t *ext.SampleConfiguration) {
			merge.MergeWithReplace(t, defaults)
		}
		newDefaults = func() *ext.SampleConfiguration {
			return util.ProtoClone(defaults)
		}
	}
	_ = newDefaults

	// These tests are similar to the base tests, but focusing on the ability to
	// use a multi-valued kv store and switch between active storages based on
	// the context key in the request.
	BeforeEach(func() {
		cs = cs.Build(newValueStore(), newKeyValueStore(), setDefaults)
	})
	keyList := []string{
		"key1",
		"key2",
		"key3",
	}
	It("should use the context key as a namespace into the active storage", func(ctx SpecContext) {
		valuesByKey := make(map[string]*ext.SampleConfiguration)
		for _, key := range keyList {
			valuesByKey[key] = mustGen()
		}
		By("setting the active configuration for each keys")
		for _, key := range keyList {
			_, err := cs.SetConfiguration(ctx, &ext.SampleSetRequest{
				Key:  &key,
				Spec: valuesByKey[key],
			})
			Expect(err).NotTo(HaveOccurred())
		}

		By("getting the active configuration for each key")
		for _, key := range keyList {
			res, err := cs.GetConfiguration(ctx, &ext.SampleGetRequest{
				Key: &key,
			})
			Expect(err).NotTo(HaveOccurred())
			expected := valuesByKey[key]
			expected.RedactSecrets()
			Expect(res.GetRevision().GetRevision()).To(BeNumerically(">", 0))
			server.UnsetRevision(res)
			Expect(res).To(testutil.ProtoEqual(expected))
		}

		By("ensuring config history records changes to each key independently")
		for _, key := range keyList {
			history, err := cs.ConfigurationHistory(ctx, &ext.SampleHistoryRequest{
				Key:           &key,
				Target:        server.Target_ActiveConfiguration,
				IncludeValues: true,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(history.Entries).To(HaveLen(1))
			Expect(history.Entries[0].GetRevision()).NotTo(BeNil())
			clone := util.ProtoClone(history.Entries[0])
			clone.Revision = nil
			Expect(clone).To(testutil.ProtoEqual(valuesByKey[key]))
		}
	})

	It("should ignore the context key when setting the default configuration", func(ctx SpecContext) {
		By("setting the default configuration multiple times")
		requests := make([]*ext.SampleConfiguration, 10)
		for i := 0; i < 10; i++ {
			requests[i] = mustGen()
			var prevRevision *corev1.Revision
			if i > 0 {
				prevRevision = requests[i-1].GetRevision()
			}
			server.SetRevision(requests[i], prevRevision.GetRevision())
			_, err := cs.SetDefaultConfiguration(ctx, &ext.SampleSetRequest{
				Key:  &keyList[i%len(keyList)],
				Spec: requests[i],
			})
			Expect(err).NotTo(HaveOccurred())

			dc, err := cs.GetDefaultConfiguration(ctx, &ext.SampleGetRequest{
				Key: &keyList[(i*3)%len(keyList)],
			})
			Expect(err).NotTo(HaveOccurred())
			server.CopyRevision(requests[i], dc)

			clone := util.ProtoClone(requests[i])
			clone.RedactSecrets()
			Expect(dc).To(testutil.ProtoEqual(clone))
		}

		By("ensuring only one default configuration exists")
		var responses []*ext.SampleConfiguration

		for i := 0; i < 10; i++ {
			res, err := cs.GetDefaultConfiguration(ctx, &ext.SampleGetRequest{
				Key: &keyList[i%len(keyList)],
			})
			Expect(err).NotTo(HaveOccurred())
			responses = append(responses, res)
		}
		// all responses should be identical, including revision
		for i := 1; i < len(responses); i++ {
			Expect(responses[i]).To(testutil.ProtoEqual(responses[0]))
		}

		By("ensuring config history correctly records all changes to the same config")
		for i := 0; i < 10; i++ {
			history, err := cs.ConfigurationHistory(ctx, &ext.SampleHistoryRequest{
				Key:           &keyList[i%len(keyList)],
				Target:        server.Target_DefaultConfiguration,
				IncludeValues: true,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(history.Entries).To(HaveLen(10))
			for j := 0; j < 10; j++ {
				Expect(history.Entries[j].GetRevision()).NotTo(BeNil())
				clone := util.ProtoClone(requests[j])
				clone.RedactSecrets()
				history.Entries[j].Revision.Timestamp = nil
				Expect(clone).To(testutil.ProtoEqual(history.Entries[j]))
			}
		}
	})

	It("should reset individual active configs using the context key", func(ctx SpecContext) {
		By("setting the active configuration for each key")
		keyList := []string{"key1", "key2", "key3"}
		valuesByKey := make(map[string]*ext.SampleConfiguration)
		for _, key := range keyList {
			valuesByKey[key] = mustGen()
			_, err := cs.SetConfiguration(ctx, &ext.SampleSetRequest{
				Key:  &key,
				Spec: valuesByKey[key],
			})
			Expect(err).NotTo(HaveOccurred())
		}

		By("setting the default configuration multiple times")
		requests := make([]*ext.SampleConfiguration, 10)
		for i := 0; i < 10; i++ {
			requests[i] = mustGen()
			var prevRevision *corev1.Revision
			if i > 0 {
				prevRevision = requests[i-1].GetRevision()
			}
			server.SetRevision(requests[i], prevRevision.GetRevision())
			_, err := cs.SetDefaultConfiguration(ctx, &ext.SampleSetRequest{
				Key:  &keyList[i%len(keyList)],
				Spec: requests[i],
			})
			Expect(err).NotTo(HaveOccurred())

			dc, err := cs.GetDefaultConfiguration(ctx, &ext.SampleGetRequest{
				Key: &keyList[(i*3)%len(keyList)],
			})
			Expect(err).NotTo(HaveOccurred())
			server.CopyRevision(requests[i], dc)
		}

		By("resetting the active configuration for each key")
		for _, key := range keyList {
			_, err := cs.ResetConfiguration(ctx, &ext.SampleResetRequest{
				Key: &key,
			})
			Expect(err).NotTo(HaveOccurred())
		}

		By("ensuring all active configurations are individually reset, but to the single default")
		for _, key := range keyList {
			res, err := cs.GetConfiguration(ctx, &ext.SampleGetRequest{
				Key: &key,
			})
			Expect(err).NotTo(HaveOccurred())
			expected := requests[len(requests)-1]
			expected.RedactSecrets()
			Expect(res.GetRevision().GetRevision()).To(BeNumerically(">", 0))
			server.UnsetRevision(res)
			server.UnsetRevision(expected)
			Expect(res).To(testutil.ProtoEqual(expected))
		}

		By("ensuring config history correctly records all changes for each key")
		for _, key := range keyList {
			history, err := cs.ConfigurationHistory(ctx, &ext.SampleHistoryRequest{
				Key:           &key,
				Target:        server.Target_ActiveConfiguration,
				IncludeValues: true,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(history.Entries).To(HaveLen(2))
			Expect(history.Entries[0].GetRevision()).NotTo(BeNil())
			Expect(history.Entries[1].GetRevision()).NotTo(BeNil())
			clone := util.ProtoClone(valuesByKey[key])
			clone.RedactSecrets()
			server.CopyRevision(clone, history.Entries[0])
			Expect(clone).To(testutil.ProtoEqual(history.Entries[0]))

			clone = util.ProtoClone(requests[len(requests)-1])
			clone.RedactSecrets()
			server.CopyRevision(clone, history.Entries[1])
			Expect(clone).To(testutil.ProtoEqual(history.Entries[1]))
		}
	})
})
