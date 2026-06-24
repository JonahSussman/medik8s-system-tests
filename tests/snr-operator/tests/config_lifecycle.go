package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/snr-operator/internal/snrparams"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe(
	"SNR Config Lifecycle tests",
	Ordered,
	ContinueOnFailure,
	Label(snrparams.Label), func() {
		BeforeAll(func() {
			By("Verify SNR deployment is ready")

			snrDeployment, err := deployment.Pull(
				APIClient, snrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get SNR deployment")
			Expect(snrDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"SNR deployment is not Ready")
		})

		Context("when SNRC watchdog path is set to an invalid path", func() {
			It("Verify SNR auto-detects softdog path",
				reportxml.ID("50770"),
				Label(labels.TierAcceptance, labels.DisruptionNonDestructive,
					labels.PlatformAny, labels.FrequencyWeekly,
					labels.ComponentController), func() {
					By("Reading current SNRC to preserve original watchdog path")

					snrc := &unstructured.Unstructured{}
					snrc.SetGroupVersionKind(snrcGVK())

					err := APIClient.Get(context.TODO(),
						client.ObjectKey{
							Name:      snrparams.SNRConfigName,
							Namespace: medik8sparams.OperatorNs,
						},
						snrc)
					Expect(err).ToNot(HaveOccurred(),
						"Failed to get default SNRC %q", snrparams.SNRConfigName)

					originalPath, found, fieldErr := unstructured.NestedString(
						snrc.Object, "spec", "watchdogFilePath")
					Expect(fieldErr).ToNot(HaveOccurred(),
						"Failed to read watchdogFilePath from SNRC")

					if !found {
						originalPath = ""
					}

					DeferCleanup(func() {
						By("DeferCleanup: restoring original watchdog path")

						pathJSON, marshalErr := json.Marshal(originalPath)
						Expect(marshalErr).ToNot(HaveOccurred(),
							"Failed to marshal watchdog path for restore patch")

						restorePatch := []byte(
							fmt.Sprintf(`{"spec":{"watchdogFilePath":%s}}`, pathJSON))

						Eventually(func() error {
							return APIClient.Patch(context.TODO(),
								snrcForPatch(snrparams.SNRConfigName),
								client.RawPatch(types.MergePatchType, restorePatch))
						}, medik8sparams.DefaultTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
							"Failed to restore original watchdog path on SNRC")

						By("DeferCleanup: waiting for DS pods to be running after restore")

						Eventually(func() error {
							return verifyDSPodsRunning()
						}, snrparams.DSPodRestartTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
							"SNR DaemonSet pods must be running after watchdog path restore")
					})

					By("Patching SNRC with invalid watchdog path /dev/foo")

					invalidPatch := []byte(`{"spec":{"watchdogFilePath":"/dev/foo"}}`)

					err = APIClient.Patch(context.TODO(),
						snrcForPatch(snrparams.SNRConfigName),
						client.RawPatch(types.MergePatchType, invalidPatch))
					Expect(err).ToNot(HaveOccurred(),
						"Failed to patch SNRC with invalid watchdog path")

					By("Waiting for SNR DaemonSet pods to restart with updated config")

					Eventually(func() error {
						return verifyDSPodsRunning()
					}, snrparams.DSPodRestartTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
						"SNR DaemonSet pods must restart after watchdog path change")

					By("Checking SNR DS pod logs for softdog auto-detection message")

					Eventually(func() error {
						return findMessageInDSPodLogs(
							snrparams.SoftdogAutoDetectMessage, snrparams.DSPodRestartTimeout)
					}, medik8sparams.DefaultTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
						"SNR DS pod logs should contain softdog auto-detection message")
				})
		})

		Context("when default SNRC is deleted", func() {
			It("Verify SNRC deletion disables SNR and recreation re-enables it",
				reportxml.ID("74298"),
				Label(labels.TierAcceptance, labels.DisruptionNonDestructive,
					labels.PlatformAny, labels.FrequencyWeekly,
					labels.ComponentController), func() {
					By("Saving current SNRC spec for later recreation")

					snrc := &unstructured.Unstructured{}
					snrc.SetGroupVersionKind(snrcGVK())

					err := APIClient.Get(context.TODO(),
						client.ObjectKey{
							Name:      snrparams.SNRConfigName,
							Namespace: medik8sparams.OperatorNs,
						},
						snrc)
					Expect(err).ToNot(HaveOccurred(),
						"Failed to get default SNRC %q", snrparams.SNRConfigName)

					savedSpec, specFound, specErr := unstructured.NestedMap(
						snrc.Object, "spec")
					Expect(specErr).ToNot(HaveOccurred(),
						"Failed to read spec from SNRC")
					Expect(specFound).To(BeTrue(),
						"SNRC %q has no spec field", snrparams.SNRConfigName)

					DeferCleanup(func() {
						By("DeferCleanup: ensuring SNRC exists")

						checkSNRC := &unstructured.Unstructured{}
						checkSNRC.SetGroupVersionKind(snrcGVK())

						getErr := APIClient.Get(context.TODO(),
							client.ObjectKey{
								Name:      snrparams.SNRConfigName,
								Namespace: medik8sparams.OperatorNs,
							},
							checkSNRC)

						if k8serrors.IsNotFound(getErr) {
							recreate := buildSNRCR("SelfNodeRemediationConfig",
								snrparams.SNRConfigName, savedSpec)

							Eventually(func() error {
								return APIClient.Create(context.TODO(), recreate)
							}, medik8sparams.DefaultTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
								"DeferCleanup: failed to recreate default SNRC")
						}

						By("DeferCleanup: waiting for DS pods to be running")

						Eventually(func() error {
							return verifyDSPodsRunning()
						}, snrparams.DSPodRestartTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
							"SNR DaemonSet pods must be running after SNRC recreation")
					})

					By("Deleting default SNRC")

					Eventually(func() error {
						deleteErr := APIClient.Delete(context.TODO(), snrc)
						if k8serrors.IsNotFound(deleteErr) {
							return nil
						}

						return deleteErr
					}, medik8sparams.DefaultTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
						"Failed to delete default SNRC")

					By("Waiting for SNR DaemonSet pods to be deleted")

					Eventually(func() error {
						return verifyDSPodsGone()
					}, snrparams.DSPodRestartTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
						"SNR DaemonSet pods must be deleted after SNRC removal")

					By("Creating SNR CR to verify config-not-found behavior")

					workerNodes, listErr := APIClient.CoreV1Interface.Nodes().List(
						context.TODO(), metav1.ListOptions{
							LabelSelector: "node-role.kubernetes.io/worker",
						})
					Expect(listErr).ToNot(HaveOccurred(), "Failed to list worker nodes")
					Expect(workerNodes.Items).ToNot(BeEmpty(), "No worker nodes found")

					testNodeName := workerNodes.Items[0].Name
					snrCR := buildSNRCR("SelfNodeRemediation", testNodeName, nil)

					createErr := APIClient.Create(context.TODO(), snrCR)
					if createErr == nil {
						deferDeleteCR(snrCR)
					}

					Expect(createErr).ToNot(HaveOccurred(),
						"Failed to create SNR CR for node %q", testNodeName)

					By("Verifying SNR status shows configuration not found")

					liveSNR := &unstructured.Unstructured{}
					liveSNR.SetGroupVersionKind(snrGVK())

					Eventually(func() error {
						getErr := APIClient.Get(context.TODO(),
							client.ObjectKey{
								Name:      testNodeName,
								Namespace: medik8sparams.OperatorNs,
							},
							liveSNR)
						if getErr != nil {
							return getErr
						}

						return verifyConditionByType(liveSNR,
							"Disabled",
							snrparams.SNRReasonConfigNotFound,
							snrparams.SNRMessageConfigNotFound)
					}, medik8sparams.DefaultTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
						"SNR status should show configuration not found")

					By("Deleting the manual SNR CR")

					Eventually(func() error {
						deleteErr := APIClient.Delete(context.TODO(), snrCR)
						if k8serrors.IsNotFound(deleteErr) {
							return nil
						}

						return deleteErr
					}, medik8sparams.DefaultTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
						"Failed to delete manual SNR CR")

					By("Recreating default SNRC")

					recreatedSNRC := buildSNRCR("SelfNodeRemediationConfig",
						snrparams.SNRConfigName, savedSpec)

					Eventually(func() error {
						return APIClient.Create(context.TODO(), recreatedSNRC)
					}, medik8sparams.DefaultTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
						"Failed to recreate default SNRC")

					By("Waiting for SNR DaemonSet pods to come back")

					Eventually(func() error {
						return verifyDSPodsRunning()
					}, snrparams.DSPodRestartTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
						"SNR DaemonSet pods must be running after SNRC recreation")
				})
		})
	})

// verifyConditionByType finds a status condition by type and checks its reason and message.
func verifyConditionByType(
	obj *unstructured.Unstructured, condType, expectedReason, expectedMessage string,
) error {
	conditions, found, err := unstructured.NestedSlice(
		obj.Object, "status", "conditions")
	if err != nil {
		return fmt.Errorf("failed to read status.conditions: %w", err)
	}

	if !found || len(conditions) == 0 {
		return fmt.Errorf("no status.conditions yet")
	}

	for _, raw := range conditions {
		condMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		typeName, _, _ := unstructured.NestedString(condMap, "type")
		if typeName != condType {
			continue
		}

		reason, _, _ := unstructured.NestedString(condMap, "reason")
		message, _, _ := unstructured.NestedString(condMap, "message")

		if reason != expectedReason {
			return fmt.Errorf("condition %q reason: expected %q, got %q",
				condType, expectedReason, reason)
		}

		if !strings.Contains(message, expectedMessage) {
			return fmt.Errorf("condition %q message: expected to contain %q, got %q",
				condType, expectedMessage, message)
		}

		return nil
	}

	return fmt.Errorf("condition with type %q not found", condType)
}
