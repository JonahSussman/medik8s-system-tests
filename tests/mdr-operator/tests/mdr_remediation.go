package tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/mdr-operator/internal/mdrparams"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("MDR Functional -- NHC-Triggered Remediation",
	Serial, Ordered, ContinueOnFailure,
	Label(labels.OperatorMDR, mdrparams.Label,
		labels.DisruptionDestructive, labels.FrequencyWeekly),
	func() {
		var (
			ctx                context.Context
			targetWorkerName   string
			initialWorkerCount int
			initialWorkerNames map[string]bool
			currentNHCName     string
			currentMDRTName    string
		)

		BeforeAll(func() {
			ctx = context.Background()

			By("Checking NHC CRD is installed")

			if !isNHCCRDInstalled() {
				Skip("NodeHealthCheck CRD not found; NHC operator not installed -- skipping MDR remediation tests")
			}

			By("Detecting cluster platform")

			platform, _, err := helpers.DetectPlatform(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())

			// MDR deletes the Machine object, which requires a cloud provider
			// to provision a replacement VM. Baremetal and None platforms
			// do not support automatic machine replacement.
			switch platform {
			case configv1.AWSPlatformType,
				configv1.AzurePlatformType,
				configv1.GCPPlatformType,
				configv1.VSpherePlatformType:
				GinkgoWriter.Printf("Platform: %s -- MachineAPI available\n", platform)
			default:
				Skip(fmt.Sprintf(
					"MDR remediation requires cloud platform with MachineAPI, got %s", platform))
			}

			By("Verifying MDR operator deployment is ready")

			mdrDeployment, err := deployment.Pull(
				APIClient, mdrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get MDR deployment")
			Expect(mdrDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"MDR deployment is not Ready")

			By("Verifying at least 2 Ready worker nodes")

			// 2 workers minimum: target (Machine deleted, VM re-created) +
			// at least 1 surviving worker so the cluster remains schedulable.
			workerCount, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(workerCount).To(BeNumerically(">=", 2),
				"MDR remediation tests require at least 2 Ready worker nodes")

			initialWorkerCount = workerCount

			By("Recording initial worker node names")

			workerNodes := &corev1.NodeList{}
			Expect(APIClient.List(ctx, workerNodes,
				client.MatchingLabels{"node-role.kubernetes.io/worker": ""})).To(Succeed())

			initialWorkerNames = make(map[string]bool, len(workerNodes.Items))
			for i := range workerNodes.Items {
				initialWorkerNames[workerNodes.Items[i].Name] = true
			}

			By("Selecting target worker node")

			targetNode, err := helpers.SelectWorkerNode(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred(), "Failed to select worker node")

			targetWorkerName = targetNode.Name
			GinkgoWriter.Printf("Target worker node: %s\n", targetWorkerName)
		})

		BeforeEach(func() {
			By("Verifying MDR operator deployment is ready")

			mdrDeployment, err := deployment.Pull(
				APIClient, mdrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get MDR deployment")
			Expect(mdrDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"MDR deployment is not Ready")

			By("Verifying target node is Ready before test")

			node := &corev1.Node{}
			Expect(APIClient.Get(ctx, client.ObjectKey{Name: targetWorkerName}, node)).To(Succeed())
			Expect(helpers.IsNodeReady(node)).To(BeTrue(),
				"Target node %s is not Ready before test", targetWorkerName)

			By("Pre-cleaning any stale CRs from previous runs")

			cleanupMDRCR(targetWorkerName)
			cleanupNHCCR(mdrparams.NHCTestName)
		})

		JustAfterEach(func() {
			// Cleanup order: CRs first (only needs API server), then node recovery.

			if currentNHCName != "" {
				By("Safety net: deleting NHC CR " + currentNHCName)
				cleanupNHCCR(currentNHCName)
				currentNHCName = ""
			}

			if currentMDRTName != "" {
				By("Safety net: deleting MDRT " + currentMDRTName)
				cleanupMDRT(currentMDRTName)
				currentMDRTName = ""
			}

			if targetWorkerName != "" {
				By("Safety net: deleting any leftover MDR CR for " + targetWorkerName)
				cleanupMDRCR(targetWorkerName)

				By("Safety net: waiting for worker count to recover")

				// MDR may have deleted the Machine, so the original node name
				// may no longer exist. Wait for the worker count to return to
				// the expected level instead of waiting for a specific node.
				Eventually(func() (int, error) {
					return helpers.CountReadyWorkerNodes(ctx, APIClient)
				}, mdrparams.NodeReadyTimeout, mdrparams.DefaultPollInterval).Should(
					BeNumerically(">=", initialWorkerCount),
					"Worker count did not recover to %d after MDR remediation", initialWorkerCount)
			}
		})

		// triggerAndWaitForRemediation is the shared flow used by both tests:
		// create MDRT + NHC, stop kubelet, wait for remediation complete.
		// Returns the replacement node name (may differ from original).
		triggerAndWaitForRemediation := func() string {
			By("Creating MDRT")

			mdrt := buildMDRT(mdrparams.MDRTestTemplateName)
			Expect(APIClient.Create(ctx, mdrt)).To(Succeed(),
				"Failed to create MDRT %s", mdrparams.MDRTestTemplateName)

			currentMDRTName = mdrparams.MDRTestTemplateName

			By("Creating NHC CR pointing to MDRT")

			nhcCR := buildNHCForMDR(mdrparams.NHCTestName, mdrparams.MDRTestTemplateName)
			Expect(APIClient.Create(ctx, nhcCR)).To(Succeed(),
				"Failed to create NHC CR %s", mdrparams.NHCTestName)

			currentNHCName = mdrparams.NHCTestName

			By(fmt.Sprintf("Stopping kubelet on worker node %s", targetWorkerName))

			Expect(stopKubeletForRemediation(ctx, targetWorkerName)).To(Succeed(),
				"Failed to stop kubelet on node %s", targetWorkerName)

			By("Waiting for MDR remediation to complete (Machine deleted, replacement node joined)")

			// MDR deletes the Machine; the cloud provider creates a new VM
			// which typically gets a DIFFERENT hostname. We find the
			// replacement by waiting for worker count to recover and
			// looking for a node not in the initial set.
			newNodeName, waitErr := waitForMDRRemediationComplete(
				ctx, targetWorkerName, initialWorkerCount, initialWorkerNames,
				mdrparams.RemediationCompleteTimeout,
			)

			Expect(waitErr).ToNot(HaveOccurred(),
				"MDR remediation did not complete for node %s within %s",
				targetWorkerName, mdrparams.RemediationCompleteTimeout)
			Expect(newNodeName).ToNot(BeEmpty(),
				"Replacement node not found after MDR remediation")

			GinkgoWriter.Printf("Replacement node: %s (original: %s)\n",
				newNodeName, targetWorkerName)

			By("Waiting for replacement node " + newNodeName + " to become Ready")

			Expect(helpers.WaitForNodeReady(
				ctx, APIClient, newNodeName,
				mdrparams.DefaultPollInterval, mdrparams.NodeReadyTimeout,
			)).To(Succeed(),
				"Replacement node %s did not become Ready after MDR remediation", newNodeName)

			By("Deleting NHC CR and MDRT")

			cleanupNHCCR(currentNHCName)
			currentNHCName = ""

			cleanupMDRT(currentMDRTName)
			currentMDRTName = ""

			// Update state so BeforeEach/JustAfterEach and subsequent tests
			// use the correct node and name set.
			delete(initialWorkerNames, targetWorkerName)
			initialWorkerNames[newNodeName] = true
			targetWorkerName = newNodeName

			return newNodeName
		}

		It("should remediate worker node via NHC-triggered Machine deletion",
			reportxml.ID("OCP-60883"),
			Label(labels.TierAcceptance,
				labels.PlatformAny, labels.ComponentRemediation),
			func() {
				triggerAndWaitForRemediation()
			})

		It("should track Processing and Succeeded conditions during remediation",
			reportxml.ID("OCP-66138"),
			Label(labels.TierAcceptance,
				labels.PlatformAny, labels.ComponentRemediation),
			func() {
				By("Creating MDRT")

				mdrt := buildMDRT(mdrparams.MDRTestTemplateName)
				Expect(APIClient.Create(ctx, mdrt)).To(Succeed(),
					"Failed to create MDRT %s", mdrparams.MDRTestTemplateName)

				currentMDRTName = mdrparams.MDRTestTemplateName

				By("Creating NHC CR pointing to MDRT")

				nhcCR := buildNHCForMDR(mdrparams.NHCTestName, mdrparams.MDRTestTemplateName)
				Expect(APIClient.Create(ctx, nhcCR)).To(Succeed(),
					"Failed to create NHC CR %s", mdrparams.NHCTestName)

				currentNHCName = mdrparams.NHCTestName

				By(fmt.Sprintf("Stopping kubelet on worker node %s", targetWorkerName))

				Expect(stopKubeletForRemediation(ctx, targetWorkerName)).To(Succeed(),
					"Failed to stop kubelet on node %s", targetWorkerName)

				By("Waiting for MDR CR to be created by NHC")

				Eventually(func() error {
					_, err := getMDRCRCondition(targetWorkerName, mdrparams.ProcessingConditionType)

					return err
				}, mdrparams.NodeNotReadyTimeout, mdrparams.DefaultPollInterval).Should(Succeed(),
					"MDR CR with Processing condition not found for node %s", targetWorkerName)

				By("Verifying Processing condition is True with RemediationStarted reason")

				Eventually(func() bool {
					cond, err := getMDRCRCondition(targetWorkerName, mdrparams.ProcessingConditionType)
					if err != nil {
						return false
					}

					return cond["status"] == "True" &&
						cond["reason"] == mdrparams.ConditionReasonRemediationStarted
				}, mdrparams.NodeNotReadyTimeout, mdrparams.DefaultPollInterval).Should(BeTrue(),
					"MDR Processing condition not True/RemediationStarted for node %s", targetWorkerName)

				GinkgoWriter.Println("MDR Processing=True, Reason=RemediationStarted")

				By("Verifying Succeeded condition is Unknown with RemediationStarted reason")

				Eventually(func() bool {
					cond, err := getMDRCRCondition(targetWorkerName, mdrparams.SucceededConditionType)
					if err != nil {
						return false
					}

					return cond["status"] == "Unknown" &&
						cond["reason"] == mdrparams.ConditionReasonRemediationStarted
				}, mdrparams.NodeNotReadyTimeout, mdrparams.DefaultPollInterval).Should(BeTrue(),
					"MDR Succeeded condition not Unknown/RemediationStarted for node %s", targetWorkerName)

				GinkgoWriter.Println("MDR Succeeded=Unknown, Reason=RemediationStarted")

				By("Waiting for MDR remediation to complete")

				newNodeName, waitErr := waitForMDRRemediationComplete(
					ctx, targetWorkerName, initialWorkerCount, initialWorkerNames,
					mdrparams.RemediationCompleteTimeout,
				)

				Expect(waitErr).ToNot(HaveOccurred(),
					"MDR remediation did not complete for node %s", targetWorkerName)

				By("Waiting for replacement node to become Ready")

				Expect(helpers.WaitForNodeReady(
					ctx, APIClient, newNodeName,
					mdrparams.DefaultPollInterval, mdrparams.NodeReadyTimeout,
				)).To(Succeed(),
					"Replacement node %s did not become Ready", newNodeName)

				delete(initialWorkerNames, targetWorkerName)
				initialWorkerNames[newNodeName] = true
				targetWorkerName = newNodeName

				By("Deleting NHC CR and MDRT")

				cleanupNHCCR(currentNHCName)
				currentNHCName = ""

				cleanupMDRT(currentMDRTName)
				currentMDRTName = ""
			})
	})
