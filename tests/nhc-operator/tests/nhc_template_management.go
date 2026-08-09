package tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/nhc-operator/internal/nhcparams"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Non-destructive template management test.
var _ = Describe("NHC Template Management -- Template Watch",
	Serial, Ordered, ContinueOnFailure,
	Label(labels.OperatorNHC, nhcparams.Label,
		labels.DisruptionNonDestructive, labels.FrequencyWeekly),
	func() {
		var ctx context.Context

		BeforeAll(func() {
			ctx = context.Background()

			By("Verifying NHC controller deployment is ready")

			nhcDeployment, err := deployment.Pull(
				APIClient, nhcparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get NHC deployment")
			Expect(nhcDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"NHC deployment is not Ready")

			By("Pre-cleaning stale CRs from previous interrupted runs")

			cleanupNHCCR(nhcparams.NHCTemplateWatchTestName)
			cleanupSNRT(nhcparams.SNRTTestName)
		})

		AfterEach(func() {
			cleanupNHCCR(nhcparams.NHCTemplateWatchTestName)
			cleanupSNRT(nhcparams.SNRTTestName)
		})

		It("Verifying NHC watches template deletion and re-creation",
			reportxml.ID("71185"),
			Label(labels.TierAcceptance, labels.PlatformAny,
				labels.ComponentController), func() {

				if !isSNRCRDInstalled(ctx) {
					Skip("SelfNodeRemediation CRD not found -- OCP-71185 requires SNRT")
				}

				nhcName := nhcparams.NHCTemplateWatchTestName
				snrtName := nhcparams.SNRTTestName

				By("Creating SNRT")

				snrt := buildSNRT(snrtName)
				Expect(APIClient.Create(ctx, snrt)).To(Succeed(),
					"Failed to create SNRT %q", snrtName)

				By("Creating NHC pointing to the test SNRT")

				nhc := buildNHCWithSNRT(nhcName, snrtName)
				Expect(APIClient.Create(ctx, nhc)).To(Succeed(),
					"Failed to create NHC %q", nhcName)

				By("Verifying NHC is Enabled with the SNRT present")

				Expect(waitForNHCPhase(ctx, nhcName, nhcparams.NHCPhaseEnabled,
					medik8sparams.DefaultTimeout)).To(Succeed(),
					"NHC %q should be Enabled when SNRT exists", nhcName)

				By("Deleting SNRT")

				cleanupSNRT(snrtName)

				By("Waiting for SNRT to be fully deleted")

				Eventually(ctx, func() (bool, error) {
					obj := &unstructured.Unstructured{}
					obj.SetGroupVersionKind(snrtGVK)

					err := APIClient.Get(ctx, client.ObjectKey{
						Name: snrtName, Namespace: medik8sparams.OperatorNs}, obj)
					if k8serrors.IsNotFound(err) {
						return true, nil
					}

					if err != nil {
						return false, err
					}

					return false, nil
				}).WithPolling(nhcparams.DefaultPollInterval).
					WithTimeout(nhcparams.RemediationCRDeletionTimeout).Should(BeTrue(),
					"SNRT %q should be deleted", snrtName)

				By("Verifying NHC transitions to Disabled after SNRT deletion")

				verifyNHCDisabledWithReason(ctx, nhcName,
					nhcparams.NHCReasonTemplateNotFound, nhcparams.NodeNotReadyTimeout)

				By("Re-creating SNRT")

				snrtRecreated := buildSNRT(snrtName)
				Expect(APIClient.Create(ctx, snrtRecreated)).To(Succeed(),
					"Failed to re-create SNRT %q", snrtName)

				By("Verifying NHC transitions back to Enabled after SNRT re-creation")

				Expect(waitForNHCPhase(ctx, nhcName, nhcparams.NHCPhaseEnabled,
					medik8sparams.DefaultTimeout)).To(Succeed(),
					"NHC %q should return to Enabled after SNRT re-creation", nhcName)
			})
	})

// Destructive custom-template test.
var _ = Describe("NHC Template Management -- Custom Remediation",
	Serial, Ordered, ContinueOnFailure,
	Label(labels.OperatorNHC, nhcparams.Label,
		labels.DisruptionDestructive, labels.FrequencyWeekly),
	func() {
		var (
			ctx              context.Context
			targetWorkerName string
		)

		BeforeAll(func() {
			ctx = context.Background()

			By("Checking SSH access is available")

			if !isSSHAvailable() {
				Skip("SSH not available -- custom template test requires SSH to stop kubelet")
			}

			By("Verifying NHC controller deployment is ready")

			nhcDeployment, err := deployment.Pull(
				APIClient, nhcparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get NHC deployment")
			Expect(nhcDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"NHC deployment is not Ready")

			By("Verifying at least 2 Ready worker nodes")

			workerCount, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(workerCount).To(BeNumerically(">=", 2),
				"Custom template test requires at least 2 Ready worker nodes")

			By("Selecting target worker node")

			targetNode, err := helpers.SelectWorkerNode(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred(), "Failed to select worker node")

			targetWorkerName = targetNode.Name
			GinkgoWriter.Printf("Target worker node: %s\n", targetWorkerName)

			By("Pre-cleaning stale CRs")

			cleanupNHCCR(nhcparams.NHCCustomTemplateTestName)
		})

		BeforeEach(func() {
			By("Verifying NHC controller deployment is ready")

			nhcDeployment, err := deployment.Pull(
				APIClient, nhcparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred())
			Expect(nhcDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"NHC deployment is not Ready")

			By("Verifying target node is Ready")

			node := &corev1.Node{}
			Expect(APIClient.Get(ctx, client.ObjectKey{Name: targetWorkerName}, node)).To(Succeed())
			Expect(helpers.IsNodeReady(node)).To(BeTrue(),
				"Target node %s is not Ready", targetWorkerName)
		})

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				logNHCControllerState()
			}

			cleanupNHCCR(nhcparams.NHCCustomTemplateTestName)

			if isSSHAvailable() {
				if sshErr := startKubeletForRemediation(ctx, targetWorkerName); sshErr != nil {
					GinkgoWriter.Printf(
						"WARNING: SSH kubelet restart failed for %s: %v\n",
						targetWorkerName, sshErr)
					AddReportEntry("ssh-kubelet-restart-failed",
						fmt.Sprintf("node %s: %v", targetWorkerName, sshErr))
				}
			}

			By("Safety net: waiting for node " + targetWorkerName + " to become Ready")

			if err := helpers.WaitForNodeReady(ctx, APIClient,
				targetWorkerName,
				nhcparams.DefaultPollInterval, nhcparams.NodeReadyTimeout,
				GinkgoWriter.Printf,
			); err != nil {
				GinkgoWriter.Printf(
					"WARNING: node %s did not become Ready within %s: %v\n",
					targetWorkerName, nhcparams.NodeReadyTimeout, err)
				AddReportEntry("safety-net-recovery-failed",
					fmt.Sprintf("node %s did not recover: %v", targetWorkerName, err))
			}
		})

		It("Verifying NHC triggers remediation with custom TRT template",
			reportxml.ID("61976"),
			Label(labels.TierAcceptance, labels.PlatformAny,
				labels.ComponentRemediation), func() {

				By("Setting up TestRemediation CRDs and RBAC")

				setupTestRemediationResources()
				DeferCleanup(cleanupTestRemediationResources)

				nhcName := nhcparams.NHCCustomTemplateTestName

				By("Counting worker nodes for status assertions")

				workerCount, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
				Expect(err).ToNot(HaveOccurred())

				expectedWorkers := int64(workerCount)

				By("Creating NHC with TestRemediation template")

				nhc := buildNHCWithTestRemediation(nhcName)
				Expect(APIClient.Create(ctx, nhc)).To(Succeed(),
					"Failed to create NHC %q with TRT", nhcName)

				By("Verifying pre-remediation status: phase=Enabled, healthyNodes and observedNodes match worker count")

				Expect(waitForNHCPhase(ctx, nhcName, nhcparams.NHCPhaseEnabled,
					medik8sparams.DefaultTimeout)).To(Succeed(),
					"NHC %q should be Enabled", nhcName)

				Eventually(func(g Gomega) {
					healthy, err := getNHCHealthyNodes(ctx, nhcName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(healthy).To(Equal(expectedWorkers),
						"healthyNodes should match worker count before remediation")
				}).WithPolling(nhcparams.DefaultPollInterval).
					WithTimeout(nhcparams.NodeNotReadyTimeout).Should(Succeed())

				Eventually(func(g Gomega) {
					observed, err := getNHCObservedNodes(ctx, nhcName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(observed).To(Equal(expectedWorkers),
						"observedNodes should match worker count")
				}).WithPolling(nhcparams.DefaultPollInterval).
					WithTimeout(nhcparams.NodeNotReadyTimeout).Should(Succeed())

				By("Stopping kubelet on target node to trigger remediation")

				Expect(stopKubeletForRemediation(ctx, targetWorkerName)).To(Succeed(),
					"Failed to stop kubelet on %s", targetWorkerName)

				By("Verifying during-remediation status: phase=Remediating, healthyNodes=workers-1, observedNodes=workers")

				Expect(waitForNHCPhase(ctx, nhcName, nhcparams.NHCPhaseRemediating,
					nhcparams.NodeNotReadyTimeout)).To(Succeed(),
					"NHC %q should enter Remediating after kubelet stop", nhcName)

				Eventually(func(g Gomega) {
					healthy, err := getNHCHealthyNodes(ctx, nhcName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(healthy).To(Equal(expectedWorkers-1),
						"healthyNodes should be workers-1 during remediation")
				}).WithPolling(nhcparams.DefaultPollInterval).
					WithTimeout(nhcparams.NodeNotReadyTimeout).Should(Succeed())

				Eventually(func(g Gomega) {
					observed, err := getNHCObservedNodes(ctx, nhcName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(observed).To(Equal(expectedWorkers),
						"observedNodes should remain unchanged during remediation")
				}).WithPolling(nhcparams.DefaultPollInterval).
					WithTimeout(nhcparams.NodeNotReadyTimeout).Should(Succeed())

				By("Verifying TestRemediation CR was created for the target node")

				Eventually(func() (bool, error) {
					return testRemediationCRExists(ctx, targetWorkerName)
				}).WithPolling(nhcparams.DefaultPollInterval).
					WithTimeout(nhcparams.NodeNotReadyTimeout).Should(BeTrue(),
					"TestRemediation CR should be created for node %s", targetWorkerName)

				By("Starting kubelet to recover the node")

				Expect(startKubeletForRemediation(ctx, targetWorkerName)).To(Succeed(),
					"Failed to start kubelet on %s", targetWorkerName)

				By("Waiting for node to become Ready")

				Expect(helpers.WaitForNodeReady(ctx, APIClient,
					targetWorkerName,
					nhcparams.DefaultPollInterval, nhcparams.NodeReadyTimeout,
					GinkgoWriter.Printf,
				)).To(Succeed(),
					"Node %s should become Ready after kubelet restart", targetWorkerName)

				By("Verifying post-recovery status: phase=Enabled, healthyNodes and observedNodes restored")

				Expect(waitForNHCPhase(ctx, nhcName, nhcparams.NHCPhaseEnabled,
					nhcparams.RemediationCompletionTimeout)).To(Succeed(),
					"NHC %q should return to Enabled after recovery", nhcName)

				Eventually(func(g Gomega) {
					healthy, err := getNHCHealthyNodes(ctx, nhcName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(healthy).To(Equal(expectedWorkers),
						"healthyNodes should be restored after recovery")
				}).WithPolling(nhcparams.DefaultPollInterval).
					WithTimeout(nhcparams.NodeNotReadyTimeout).Should(Succeed())

				Eventually(func(g Gomega) {
					observed, err := getNHCObservedNodes(ctx, nhcName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(observed).To(Equal(expectedWorkers),
						"observedNodes should be unchanged after recovery")
				}).WithPolling(nhcparams.DefaultPollInterval).
					WithTimeout(nhcparams.NodeNotReadyTimeout).Should(Succeed())

				By("Verifying TestRemediation CR was cleaned up")

				Eventually(func() (bool, error) {
					exists, err := testRemediationCRExists(ctx, targetWorkerName)

					return !exists, err
				}).WithPolling(nhcparams.DefaultPollInterval).
					WithTimeout(nhcparams.RemediationCRDeletionTimeout).Should(BeTrue(),
					"TestRemediation CR should be deleted after recovery")
			})
	})
