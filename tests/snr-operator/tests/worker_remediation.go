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
	"github.com/medik8s/system-tests/tests/snr-operator/internal/snrparams"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("SNR Functional - Worker Node Remediation",
	Serial, Ordered, ContinueOnFailure,
	Label(labels.OperatorSNR, snrparams.Label,
		labels.DisruptionDestructive, labels.FrequencyNightly),
	func() {
		var (
			ctx              context.Context
			targetWorkerName string
			currentNHCName   string
			currentSNRTName  string
		)

		BeforeAll(func() {
			ctx = context.Background()

			By("Checking NHC CRD is installed")

			if !isNHCCRDInstalled() {
				Skip("NodeHealthCheck CRD not found; NHC operator not installed -- skipping worker remediation tests")
			}

			By("Verifying SNR operator deployment is ready")

			snrDeployment, err := deployment.Pull(
				APIClient, snrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get SNR deployment")
			Expect(snrDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"SNR deployment is not Ready")

			By("Verifying at least 2 Ready worker nodes")

			// 2 workers minimum: target (kubelet stopped, rebooted) + at least
			// 1 surviving worker so the cluster remains schedulable and pods
			// can be evicted to a healthy node (Tests 8/9).
			workerCount, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(workerCount).To(BeNumerically(">=", 2),
				"Worker remediation tests require at least 2 Ready worker nodes")

			By("Selecting target worker node")

			targetNode, err := helpers.SelectWorkerNode(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred(), "Failed to select worker node")

			targetWorkerName = targetNode.Name
			GinkgoWriter.Printf("Target worker node: %s\n", targetWorkerName)
		})

		JustAfterEach(func() {
			// Cleanup order: CRs first (only needs API server), then node
			// recovery. If node recovery Expect aborts, CRs are already
			// cleaned up.

			if currentNHCName != "" {
				By("Safety net: deleting NHC CR " + currentNHCName)
				cleanupNHCCR(currentNHCName)
				currentNHCName = ""
			}

			if currentSNRTName != "" {
				By("Safety net: deleting SNRT " + currentSNRTName)
				cleanupSNRT(currentSNRTName)
				currentSNRTName = ""
			}

			if targetWorkerName != "" {
				By("Safety net: deleting any leftover SNR CR for " + targetWorkerName)
				cleanupSNRCR(targetWorkerName)

				By("Safety net: waiting for node " + targetWorkerName + " to become Ready")

				if err := helpers.WaitForNodeReady(
					ctx, APIClient, targetWorkerName,
					snrparams.DefaultPollInterval, snrparams.NodeReadyTimeout,
				); err != nil {
					GinkgoWriter.Printf(
						"WARNING: node %s did not become Ready within %s: %v\n",
						targetWorkerName, snrparams.NodeReadyTimeout, err)
					AddReportEntry("safety-net-recovery-failed",
						fmt.Sprintf("node %s did not recover: %v", targetWorkerName, err))
				}
			}

			By("Safety net: verifying SNR DS pods are running")

			Eventually(func() error {
				return verifyDSPodsRunning()
			}, snrparams.DSPodRestartTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
				"SNR DaemonSet pods did not recover after remediation")
		})

		It("should remediate a worker node after kubelet stop via NHC detection",
			reportxml.ID("OCP-52416"),
			Label(labels.TierAcceptance, labels.DisruptionDestructive,
				labels.PlatformAny, labels.ComponentRemediation),
			func() {
				By("Recording boot ID before remediation")

				oldBootID, err := helpers.GetNodeBootIDFromAPI(ctx, APIClient, targetWorkerName)
				Expect(err).ToNot(HaveOccurred(),
					"Must read boot ID from node %s", targetWorkerName)
				GinkgoWriter.Printf("Pre-remediation boot ID: %s\n", oldBootID)

				By("Recording node creation timestamp and verifying node is Ready")

				node := &corev1.Node{}
				Expect(APIClient.Get(ctx, client.ObjectKey{Name: targetWorkerName}, node)).To(Succeed())
				Expect(helpers.IsNodeReady(node)).To(BeTrue(),
					"Target node %s is not Ready before test", targetWorkerName)

				creationTimestamp := node.CreationTimestamp

				By("Pre-cleaning any stale NHC CR from previous runs")

				cleanupNHCCR(snrparams.NHCTestName)

				By("Creating NHC CR pointing to default Automatic SNRT")

				nhcCR := buildNHCForWorkers(snrparams.NHCTestName, snrparams.SNRTemplateName)
				Expect(APIClient.Create(ctx, nhcCR)).To(Succeed(),
					"Failed to create NHC CR %s", snrparams.NHCTestName)

				currentNHCName = snrparams.NHCTestName

				By(fmt.Sprintf("Stopping kubelet on worker node %s", targetWorkerName))

				Expect(stopKubeletForRemediation(ctx, targetWorkerName)).To(Succeed(),
					"Failed to stop kubelet on node %s", targetWorkerName)

				By("Waiting for SNR remediation to complete (node rebooted, SNR CR gone)")

				// The full cycle: NHC detects NotReady (60s) -> creates SNR CR ->
				// SNR reboots node -> node recovers -> SNR CR deleted.
				// On fast clusters or when StopKubelet takes long (ARM64 oc debug
				// timeout), the entire cycle may complete before we start checking.
				// waitForRemediationComplete handles both cases.
				Expect(waitForRemediationComplete(
					ctx, APIClient, targetWorkerName, oldBootID, snrparams.SNRDeletionTimeout,
				)).To(Succeed(),
					"SNR remediation did not complete for node %s within %s",
					targetWorkerName, snrparams.SNRDeletionTimeout)

				By("Waiting for node " + targetWorkerName + " to become Ready")

				Expect(helpers.WaitForNodeReady(
					ctx, APIClient, targetWorkerName,
					snrparams.DefaultPollInterval, snrparams.NodeReadyTimeout,
				)).To(Succeed(),
					"Node %s did not become Ready after remediation", targetWorkerName)

				By("Verifying boot ID changed (node rebooted)")

				newBootID, err := helpers.GetNodeBootIDFromAPI(ctx, APIClient, targetWorkerName)
				Expect(err).ToNot(HaveOccurred(),
					"Failed to read post-remediation boot ID from node %s", targetWorkerName)
				Expect(newBootID).ToNot(Equal(oldBootID),
					"Boot ID unchanged -- node %s did not reboot (old: %s, new: %s)",
					targetWorkerName, oldBootID, newBootID)
				GinkgoWriter.Printf("Boot ID changed: %s -> %s\n", oldBootID, newBootID)

				By("Verifying node creation timestamp unchanged (node was rebooted, not deleted)")

				updatedNode := &corev1.Node{}
				Expect(APIClient.Get(ctx, client.ObjectKey{Name: targetWorkerName}, updatedNode)).To(Succeed())
				Expect(updatedNode.CreationTimestamp.Equal(&creationTimestamp)).To(BeTrue(),
					"Node creation timestamp changed -- node was re-created instead of rebooted")

				By("Verifying OutOfServiceTaint auto-selected log message")

				Eventually(func() error {
					return findMessageInControllerLogs(
						snrparams.OutOfServiceAutoSelectedMsg, snrparams.DSLogSearchWindow)
				}, medik8sparams.DefaultTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
					"OutOfServiceTaint auto-selected message not found in SNR controller logs")

				By("Deleting NHC CR")

				cleanupNHCCR(currentNHCName)
				currentNHCName = ""
			})

		It("should evict workload pod using ResourceDeletion remediation strategy",
			reportxml.ID("OCP-50772"),
			Label(labels.TierAcceptance, labels.DisruptionDestructive,
				labels.PlatformAny, labels.ComponentRemediation),
			func() {
				By("Checking at least 2 Ready workers for pod eviction")

				workerCount, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
				Expect(err).ToNot(HaveOccurred())

				if workerCount < 2 {
					Skip(fmt.Sprintf(
						"ResourceDeletion test requires 2+ workers for pod eviction, got %d",
						workerCount))
				}

				By("Pre-cleaning any stale SNRT from previous runs")

				cleanupSNRT(snrparams.SNRTResourceDeletionName)

				By("Creating ResourceDeletion SNRT")

				snrt := buildSNRT(snrparams.SNRTResourceDeletionName, "ResourceDeletion")
				Expect(APIClient.Create(ctx, snrt)).To(Succeed(),
					"Failed to create ResourceDeletion SNRT")

				currentSNRTName = snrparams.SNRTResourceDeletionName

				By(fmt.Sprintf("Creating test workload pod on node %s", targetWorkerName))

				workloadPod := createWorkloadPodOnNode(ctx, targetWorkerName)

				By("Recording boot ID before remediation")

				oldBootID, err := helpers.GetNodeBootIDFromAPI(ctx, APIClient, targetWorkerName)
				Expect(err).ToNot(HaveOccurred())

				By("Pre-cleaning any stale NHC CR from previous runs")

				cleanupNHCCR(snrparams.NHCTestName)

				By("Creating NHC CR pointing to ResourceDeletion SNRT")

				nhcCR := buildNHCForWorkers(snrparams.NHCTestName, snrparams.SNRTResourceDeletionName)
				Expect(APIClient.Create(ctx, nhcCR)).To(Succeed(),
					"Failed to create NHC CR")

				currentNHCName = snrparams.NHCTestName

				By(fmt.Sprintf("Stopping kubelet on worker node %s", targetWorkerName))

				Expect(stopKubeletForRemediation(ctx, targetWorkerName)).To(Succeed())

				By("Waiting for SNR remediation to complete (node rebooted, SNR CR gone)")

				Expect(waitForRemediationComplete(
					ctx, APIClient, targetWorkerName, oldBootID, snrparams.SNRDeletionTimeout,
				)).To(Succeed(),
					"SNR remediation did not complete for node %s", targetWorkerName)

				By("Waiting for node to become Ready")

				Expect(helpers.WaitForNodeReady(
					ctx, APIClient, targetWorkerName,
					snrparams.DefaultPollInterval, snrparams.NodeReadyTimeout,
				)).To(Succeed())

				By("Verifying workload pod was evicted from remediated node")

				waitForPodEvictedFromNode(ctx,
					workloadPod.Name, workloadPod.Namespace, targetWorkerName)

				By("Cleaning up NHC CR and SNRT")

				cleanupNHCCR(currentNHCName)
				currentNHCName = ""

				cleanupSNRT(currentSNRTName)
				currentSNRTName = ""
			})

		It("should evict workload pod using OutOfServiceTaint remediation strategy",
			reportxml.ID("OCP-61594"),
			Label(labels.TierAcceptance, labels.DisruptionDestructive,
				labels.PlatformAny, labels.ComponentRemediation),
			func() {
				By("Checking at least 2 Ready workers for pod eviction")

				workerCount, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
				Expect(err).ToNot(HaveOccurred())

				if workerCount < 2 {
					Skip(fmt.Sprintf(
						"OutOfServiceTaint test requires 2+ workers for pod eviction, got %d",
						workerCount))
				}

				By("Pre-cleaning any stale SNRT from previous runs")

				cleanupSNRT(snrparams.SNRTOutOfServiceTaintName)

				By("Creating OutOfServiceTaint SNRT")

				snrt := buildSNRT(snrparams.SNRTOutOfServiceTaintName, "OutOfServiceTaint")
				Expect(APIClient.Create(ctx, snrt)).To(Succeed(),
					"Failed to create OutOfServiceTaint SNRT")

				currentSNRTName = snrparams.SNRTOutOfServiceTaintName

				By(fmt.Sprintf("Creating test workload pod on node %s", targetWorkerName))

				workloadPod := createWorkloadPodOnNode(ctx, targetWorkerName)

				By("Recording boot ID before remediation")

				oldBootID, err := helpers.GetNodeBootIDFromAPI(ctx, APIClient, targetWorkerName)
				Expect(err).ToNot(HaveOccurred())

				By("Pre-cleaning any stale NHC CR from previous runs")

				cleanupNHCCR(snrparams.NHCTestName)

				By("Creating NHC CR pointing to OutOfServiceTaint SNRT")

				nhcCR := buildNHCForWorkers(snrparams.NHCTestName, snrparams.SNRTOutOfServiceTaintName)
				Expect(APIClient.Create(ctx, nhcCR)).To(Succeed())

				currentNHCName = snrparams.NHCTestName

				By(fmt.Sprintf("Stopping kubelet on worker node %s", targetWorkerName))

				Expect(stopKubeletForRemediation(ctx, targetWorkerName)).To(Succeed())

				By("Waiting for SNR remediation to complete (node rebooted, SNR CR gone)")

				Expect(waitForRemediationComplete(
					ctx, APIClient, targetWorkerName, oldBootID, snrparams.SNRDeletionTimeout,
				)).To(Succeed(),
					"SNR remediation did not complete for node %s", targetWorkerName)

				By("Waiting for node to become Ready")

				Expect(helpers.WaitForNodeReady(
					ctx, APIClient, targetWorkerName,
					snrparams.DefaultPollInterval, snrparams.NodeReadyTimeout,
				)).To(Succeed())

				By("Verifying workload pod was evicted from remediated node")

				waitForPodEvictedFromNode(ctx,
					workloadPod.Name, workloadPod.Namespace, targetWorkerName)

				By("Cleaning up NHC CR and SNRT")

				cleanupNHCCR(currentNHCName)
				currentNHCName = ""

				cleanupSNRT(currentSNRTName)
				currentSNRTName = ""
			})
	})
