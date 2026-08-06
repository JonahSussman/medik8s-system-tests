package tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/nhc-operator/internal/nhcparams"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("NHC Negative -- Validation and Webhook",
	Serial, Ordered, ContinueOnFailure,
	Label(labels.OperatorNHC, nhcparams.Label),
	func() {
		var ctx context.Context

		// allNegativeTestNames lists every NHC CR name used by tests in this
		// Describe so AfterEach can sweep-clean in a single place.
		allNegativeTestNames := []string{
			nhcparams.NHCDuplicateTestName,
			nhcparams.NHCIncorrectTemplateTestName,
			nhcparams.NHCInvalidValuesTestName,
			nhcparams.NHCMissingNsTestName,
			nhcparams.NHCEmptySelectorTestName,
		}

		BeforeAll(func() {
			ctx = context.Background()

			By("Verifying NHC controller deployment is ready")

			nhcDeployment, err := deployment.Pull(
				APIClient, nhcparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get NHC deployment")
			Expect(nhcDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"NHC deployment is not Ready")

			By("Pre-cleaning stale NHC CRs from previous interrupted runs")

			for _, name := range allNegativeTestNames {
				cleanupNHCCR(name)
			}
		})

		AfterEach(func() {
			for _, name := range allNegativeTestNames {
				cleanupNHCCR(name)
			}
		})

		Context("webhook rejection", func() {
			// OCP-53769 tests standard K8s AlreadyExists behavior for NHC CRs.
			// This is a Polarion requirement, not NHC-specific webhook validation.
			It("Verifying duplicate NHC name creation is rejected",
				reportxml.ID("53769"),
				Label(labels.TierAcceptance, labels.DisruptionNonDestructive,
					labels.PlatformAny, labels.FrequencyWeekly,
					labels.ComponentWebhook), func() {

					nhcName := nhcparams.NHCDuplicateTestName

					By("Creating first NHC CR")

					nhc := buildNHCForWorkers(nhcName)
					Expect(APIClient.Create(ctx, nhc)).To(Succeed(),
						"Failed to create first NHC CR %q", nhcName)

					By("Verifying NHC was created")

					created := &unstructured.Unstructured{}
					created.SetGroupVersionKind(nhcGVK)
					Expect(APIClient.Get(ctx, client.ObjectKey{Name: nhcName}, created)).To(Succeed(),
						"NHC CR %q should exist after creation", nhcName)

					By("Attempting to create second NHC with the same name")

					duplicate := buildNHCForWorkers(nhcName)
					err := APIClient.Create(ctx, duplicate)
					Expect(err).To(HaveOccurred(), "Duplicate NHC creation should fail")
					Expect(k8serrors.IsAlreadyExists(err)).To(BeTrue(),
						"Expected AlreadyExists error, got: %v", err)

					By("Verifying only one NHC with this name exists")

					nhcList := &unstructured.UnstructuredList{}
					nhcList.SetGroupVersionKind(nhcGVK)
					Expect(APIClient.List(ctx, nhcList)).To(Succeed(),
						"Failed to list NHC CRs")

					count := 0
					for i := range nhcList.Items {
						if nhcList.Items[i].GetName() == nhcName {
							count++
						}
					}

					Expect(count).To(Equal(1),
						"Expected exactly 1 NHC CR named %q, found %d", nhcName, count)
				})

			// OCP-51626 checks both minHealthy and unhealthyConditions in the same
			// assertion. The NHC webhook currently aggregates all validation errors
			// into a single response. If it switches to fail-fast, split into
			// per-field tests.
			It("Verifying NHC creation with invalid values is rejected",
				reportxml.ID("51626"),
				Label(labels.TierAcceptance, labels.DisruptionNonDestructive,
					labels.PlatformAny, labels.FrequencyWeekly,
					labels.ComponentWebhook), func() {

					nhcName := nhcparams.NHCInvalidValuesTestName

					By("Creating NHC with negative minHealthy and duration values")

					nhc := buildNHCForWorkers(nhcName)
					spec := nhc.Object["spec"].(map[string]interface{})
					spec["minHealthy"] = "-30%"

					conditions := spec["unhealthyConditions"].([]interface{})
					conditions[0].(map[string]interface{})["duration"] = "-30s"

					err := APIClient.Create(ctx, nhc)
					Expect(err).To(HaveOccurred(), "NHC creation with negative values should fail")
					Expect(err).To(MatchError(ContainSubstring("spec.minHealthy")),
						"Error should mention invalid minHealthy")
					Expect(err).To(MatchError(ContainSubstring("spec.unhealthyConditions")),
						"Error should mention invalid unhealthyConditions duration")

					By("Verifying NHC was not created")

					verifyNHCNotCreated(ctx, nhcName)

					By("Creating NHC with string minHealthy and duration values")

					nhcStr := buildNHCForWorkers(nhcName)
					specStr := nhcStr.Object["spec"].(map[string]interface{})
					specStr["minHealthy"] = "string"

					conditionsStr := specStr["unhealthyConditions"].([]interface{})
					conditionsStr[0].(map[string]interface{})["duration"] = "string"

					err = APIClient.Create(ctx, nhcStr)
					Expect(err).To(HaveOccurred(), "NHC creation with string values should fail")
					Expect(err).To(MatchError(ContainSubstring("spec.minHealthy")),
						"Error should mention invalid minHealthy")
					Expect(err).To(MatchError(ContainSubstring("spec.unhealthyConditions")),
						"Error should mention invalid unhealthyConditions duration")

					By("Verifying NHC was not created after string values attempt")

					verifyNHCNotCreated(ctx, nhcName)
				})

			It("Verifying NHC creation with empty selector is rejected",
				reportxml.ID("61591"),
				Label(labels.TierAcceptance, labels.DisruptionNonDestructive,
					labels.PlatformAny, labels.FrequencyWeekly,
					labels.ComponentWebhook), func() {

					nhcName := nhcparams.NHCEmptySelectorTestName

					By("Creating NHC with empty matchExpressions selector")

					// buildNHC with nil matchLabels creates an intermediate selector that
					// is immediately overwritten below -- we only need the base NHC spec.
					nhc := buildNHC(nhcName, "", "", nil)
					nhc.Object["spec"].(map[string]interface{})["selector"] = map[string]interface{}{
						"matchExpressions": []interface{}{},
					}

					err := APIClient.Create(ctx, nhc)
					Expect(err).To(HaveOccurred(), "NHC creation with empty selector should fail")
					Expect(err).To(MatchError(ContainSubstring("Selector is mandatory")),
						"Error should indicate selector is mandatory")

					By("Verifying NHC was not created")

					verifyNHCNotCreated(ctx, nhcName)
				})
		})

		Context("controller behavior", func() {
			// OCP-51625 covers two scenarios (wrong template name + wrong API group)
			// under a single Polarion ID. Both are kept in one It block to avoid
			// duplicating the reportxml.ID -- duplicate IDs cause one Polarion
			// result to overwrite the other.
			It("Verifying NHC reports Disabled phase for non-existent remediation template",
				reportxml.ID("51625"),
				Label(labels.TierAcceptance, labels.DisruptionNonDestructive,
					labels.PlatformAny, labels.FrequencyWeekly,
					labels.ComponentController), func() {

					nhcName := nhcparams.NHCIncorrectTemplateTestName

					By("Creating NHC with non-existent SNR template name")

					nhc := buildNHCForWorkers(nhcName)
					spec := nhc.Object["spec"].(map[string]interface{})
					spec["remediationTemplate"].(map[string]interface{})["name"] = "non-existent-template"

					Expect(APIClient.Create(ctx, nhc)).To(Succeed(),
						"NHC creation should succeed even with a non-existent template")

					By("Verifying NHC status phase is Disabled")

					Expect(waitForNHCPhase(ctx, nhcName, nhcparams.NHCPhaseDisabled,
						nhcparams.NodeNotReadyTimeout)).To(Succeed(),
						"NHC %q should reach Disabled phase", nhcName)

					By("Verifying NHC status reason is RemediationTemplateNotFound")

					Eventually(func(g Gomega) {
						reason, err := getNHCReason(ctx, nhcName)
						g.Expect(err).ToNot(HaveOccurred(), "Failed to get NHC reason")
						g.Expect(reason).To(ContainSubstring("RemediationTemplateNotFound"),
							"NHC reason should indicate template not found")
					}).WithPolling(nhcparams.DefaultPollInterval).
						WithTimeout(nhcparams.NodeNotReadyTimeout).Should(Succeed())

					// Must delete and confirm gone before reusing the same name.
					By("Deleting NHC with wrong SNR template before next scenario")

					cleanupNHCCR(nhcName)
					waitForNHCGone(ctx, nhcName)

					By("Creating NHC with poison-pill remediation template (non-existent API group)")

					nhcPP := buildNHCForWorkers(nhcName)
					specPP := nhcPP.Object["spec"].(map[string]interface{})
					specPP["remediationTemplate"] = map[string]interface{}{
						"apiVersion": "poison-pill-remediation.medik8s.io/v1alpha1",
						"kind":       "PoisonPillRemediationTemplate",
						"name":       "poison-pill-default-template",
						"namespace":  medik8sparams.OperatorNs,
					}

					Expect(APIClient.Create(ctx, nhcPP)).To(Succeed(),
						"NHC creation should succeed even with a non-existent API group")

					By("Verifying NHC status phase is Disabled for poison-pill template")

					Expect(waitForNHCPhase(ctx, nhcName, nhcparams.NHCPhaseDisabled,
						nhcparams.NodeNotReadyTimeout)).To(Succeed(),
						"NHC %q should reach Disabled phase for poison-pill template", nhcName)

					By("Verifying NHC status reason is RemediationTemplateNotFound for poison-pill template")

					Eventually(func(g Gomega) {
						reason, err := getNHCReason(ctx, nhcName)
						g.Expect(err).ToNot(HaveOccurred(), "Failed to get NHC reason")
						g.Expect(reason).To(ContainSubstring("RemediationTemplateNotFound"),
							"NHC reason should indicate template not found for poison-pill template")
					}).WithPolling(nhcparams.DefaultPollInterval).
						WithTimeout(nhcparams.NodeNotReadyTimeout).Should(Succeed())
				})

			It("Verifying NHC handles missing template namespace correctly",
				reportxml.ID("71184"),
				Label(labels.TierAcceptance, labels.DisruptionNonDestructive,
					labels.PlatformAny, labels.FrequencyWeekly,
					labels.ComponentController), func() {

					nhcName := nhcparams.NHCMissingNsTestName

					By("Part 1: Namespaced template (SNRT) without namespace in remediationTemplate")

					By("Creating NHC with SNRT but no namespace in remediationTemplate ref")

					nhc := buildNHCForWorkers(nhcName)
					spec := nhc.Object["spec"].(map[string]interface{})
					tmpl := spec["remediationTemplate"].(map[string]interface{})
					delete(tmpl, "namespace")

					Expect(APIClient.Create(ctx, nhc)).To(Succeed(),
						"NHC creation should succeed without namespace in template ref")

					By("Verifying NHC is Disabled due to missing namespace")

					Expect(waitForNHCPhase(ctx, nhcName, nhcparams.NHCPhaseDisabled,
						nhcparams.NodeNotReadyTimeout)).To(Succeed(),
						"NHC %q should be Disabled when SNRT namespace is missing", nhcName)

					Eventually(func(g Gomega) {
						reason, err := getNHCReason(ctx, nhcName)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(reason).To(ContainSubstring("RemediationTemplateNotFound"),
							"NHC reason should indicate template not found")
					}).WithPolling(nhcparams.DefaultPollInterval).
						WithTimeout(nhcparams.NodeNotReadyTimeout).Should(Succeed())

					By("Adding namespace to remediationTemplate via patch")

					patchBytes := []byte(fmt.Sprintf(
						`{"spec":{"remediationTemplate":{"namespace":%q}}}`,
						medik8sparams.OperatorNs))
					patchedNHC := &unstructured.Unstructured{}
					patchedNHC.SetGroupVersionKind(nhcGVK)
					patchedNHC.SetName(nhcName)
					Expect(APIClient.Patch(ctx, patchedNHC,
						client.RawPatch(types.MergePatchType, patchBytes))).To(Succeed(),
						"Failed to patch namespace into NHC remediationTemplate")

					By("Verifying NHC transitions to Enabled after namespace added")

					Expect(waitForNHCPhase(ctx, nhcName, nhcparams.NHCPhaseEnabled,
						nhcparams.NodeNotReadyTimeout)).To(Succeed(),
						"NHC %q should become Enabled after adding namespace", nhcName)

					By("Removing namespace from remediationTemplate via patch")

					// JSON merge patch with null removes the key entirely (RFC 7396),
					// matching the Python reference which uses del on the namespace field.
					removePatch := []byte(`{"spec":{"remediationTemplate":{"namespace":null}}}`)
					Expect(APIClient.Patch(ctx, patchedNHC,
						client.RawPatch(types.MergePatchType, removePatch))).To(Succeed(),
						"Failed to remove namespace from NHC remediationTemplate")

					By("Verifying NHC transitions back to Disabled after namespace removed")

					Expect(waitForNHCPhase(ctx, nhcName, nhcparams.NHCPhaseDisabled,
						nhcparams.NodeNotReadyTimeout)).To(Succeed(),
						"NHC %q should return to Disabled after removing namespace", nhcName)

					By("Verifying NHC reason is RemediationTemplateNotFound after re-disable")

					Eventually(func(g Gomega) {
						reason, err := getNHCReason(ctx, nhcName)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(reason).To(ContainSubstring("RemediationTemplateNotFound"),
							"NHC reason should indicate template not found after namespace removal")
					}).WithPolling(nhcparams.DefaultPollInterval).
						WithTimeout(nhcparams.NodeNotReadyTimeout).Should(Succeed())

					By("Cleaning up NHC before Part 2")

					cleanupNHCCR(nhcName)
					waitForNHCGone(ctx, nhcName)

					By("Part 2: Cluster-scoped template (TRT) without namespace")

					By("Setting up TestRemediation CRDs and RBAC")

					setupTestRemediationResources()
					DeferCleanup(cleanupTestRemediationResources)

					By("Creating NHC with cluster-scoped TRT and no namespace in ref")

					nhcTRT := buildNHCForWorkers(nhcName)
					specTRT := nhcTRT.Object["spec"].(map[string]interface{})
					specTRT["remediationTemplate"] = map[string]interface{}{
						"apiVersion": nhcparams.TestRemediationGroup + "/" + nhcparams.TestRemediationVersion,
						"kind":       "TestRemediationTemplate",
						"name":       nhcparams.TestRemediationTemplateName,
					}

					Expect(APIClient.Create(ctx, nhcTRT)).To(Succeed(),
						"NHC creation with cluster-scoped TRT should succeed without namespace")

					By("Verifying NHC with cluster-scoped TRT is Enabled")

					Expect(waitForNHCPhase(ctx, nhcName, nhcparams.NHCPhaseEnabled,
						nhcparams.NodeNotReadyTimeout)).To(Succeed(),
						"NHC %q should be Enabled with cluster-scoped TRT (no namespace needed)", nhcName)
				})
		})
	})
