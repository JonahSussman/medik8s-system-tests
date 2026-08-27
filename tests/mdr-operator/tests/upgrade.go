package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	olmV1alpha1 "github.com/rh-ecosystem-edge/eco-goinfra/pkg/schemes/olm/operators/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/mdr-operator/internal/mdrparams"
	"github.com/medik8s/system-tests/tests/mdr-operator/internal/mdrutils"
)

// Unlike FAR/SNR, MDR has no standalone remediation trigger -- it is always
// invoked by NHC via a MachineDeletionRemediationTemplate. This upgrade test
// therefore requires the NHC operator to also be installed on the cluster,
// mirroring the existing MDR functional test's dependency (see mdr_remediation.go).
var _ = Describe("MDR Operator Upgrade",
	Serial, Ordered,
	Label(labels.OperatorMDR, mdrparams.Label,
		labels.TierUpgrade, labels.DisruptionDestructive,
		labels.PlatformAny, labels.FrequencyWeekly,
		labels.ComponentOLM),
	func() {
		var (
			ctx                context.Context
			previousCSV        *olm.ClusterServiceVersionBuilder
			preUpgradeImage    string
			initialWorkerCount int
			initialWorkerNames map[string]bool
			targetWorkerName   string
		)

		BeforeAll(func() {
			ctx = context.Background()

			Expect(medik8sparams.TargetOCPImage).NotTo(BeEmpty(),
				"OPENSHIFT_UPGRADE_RELEASE_IMAGE_OVERRIDE or RELEASE_IMAGE_LATEST must be set")

			By("Checking NHC CRD is installed")

			if !isNHCCRDInstalled() {
				Skip("NodeHealthCheck CRD not found; NHC operator not installed -- " +
					"skipping MDR upgrade test (MDR remediation is always NHC-triggered)")
			}

			By("Detecting cluster platform")

			platform, _, err := helpers.DetectPlatform(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())

			switch platform {
			case configv1.AWSPlatformType,
				configv1.AzurePlatformType,
				configv1.GCPPlatformType,
				configv1.VSpherePlatformType:
				GinkgoWriter.Printf("Platform: %s -- MachineAPI available\n", platform)
			default:
				Skip(fmt.Sprintf(
					"MDR upgrade test requires cloud platform with MachineAPI, got %s", platform))
			}

			By("Verifying at least 2 Ready worker nodes")

			workerCount, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(workerCount).To(BeNumerically(">=", 2),
				"MDR upgrade test requires at least 2 Ready worker nodes")

			initialWorkerCount = workerCount

			By("Recording initial worker node names")

			workerNodes := &corev1.NodeList{}
			Expect(APIClient.List(ctx, workerNodes,
				client.MatchingLabels{"node-role.kubernetes.io/worker": ""})).To(Succeed())

			initialWorkerNames = make(map[string]bool, len(workerNodes.Items))
			for i := range workerNodes.Items {
				initialWorkerNames[workerNodes.Items[i].Name] = true
			}

			By("Creating shared MDRT and NHC CR for upgrade remediation cycles")

			mdrt := buildMDRT(mdrparams.UpgradeMDRTName)
			Expect(APIClient.Create(ctx, mdrt)).To(Succeed(),
				"Failed to create MDRT %s", mdrparams.UpgradeMDRTName)

			nhcCR := buildNHCForMDR(mdrparams.UpgradeNHCName, mdrparams.UpgradeMDRTName)
			Expect(APIClient.Create(ctx, nhcCR)).To(Succeed(),
				"Failed to create NHC CR %s", mdrparams.UpgradeNHCName)
		})

		AfterAll(func() {
			cleanupNHCCR(mdrparams.UpgradeNHCName)
			cleanupMDRT(mdrparams.UpgradeMDRTName)
			mdrutils.CleanupUpgradeResources(APIClient, GinkgoWriter.Printf)
		})

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				logMDRControllerState()
			}

			if targetWorkerName != "" {
				By("Safety net: deleting any leftover MDR CR for " + targetWorkerName)
				cleanupMDRCR(targetWorkerName)

				By("Safety net: waiting for worker count to recover")

				Eventually(func() (int, error) {
					return helpers.CountReadyWorkerNodes(ctx, APIClient)
				}, mdrparams.NodeReadyTimeout, mdrparams.DefaultPollInterval).Should(
					BeNumerically(">=", initialWorkerCount),
					"Worker count did not recover to %d after MDR remediation", initialWorkerCount)
			}
		})

		It("should survive OCP upgrade and operator upgrade with working remediation",
			Label(labels.ComponentRemediation),
			func() {
				By("Step 1: Install MDR operator GA version from redhat-operators")

				_, err := mdrutils.InstallGAOperator(APIClient)
				Expect(err).NotTo(HaveOccurred(), "Failed to install GA MDR operator")

				By("Step 2: Wait for CSV to reach Succeeded and deployment to be Ready")

				Eventually(func() error {
					var csvErr error
					previousCSV, csvErr = mdrutils.FindSucceededCSV(
						APIClient, medik8sparams.OperatorPackage)

					return csvErr
				}, medik8sparams.OperatorUpgradeTimeout, mdrparams.DefaultPollInterval).Should(Succeed(),
					"MDR CSV must reach Succeeded phase")

				mdrDeploy, err := deployment.Pull(
					APIClient, mdrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
				Expect(err).NotTo(HaveOccurred(), "Failed to get MDR deployment")
				Expect(mdrDeploy.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
					"MDR deployment is not Ready")

				By("Step 3: Record GA install checkpoint")

				preUpgradeImage, err = mdrutils.GetMDRControllerImage(APIClient)
				Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("GA operator image: %s\n", preUpgradeImage)
				GinkgoWriter.Printf("GA MDR CSV: %s\n", previousCSV.Object.Name)

				By("Step 4: Upgrade OCP from N-1 to N")

				upgradeOCP(ctx, medik8sparams.TargetOCPImage)

				By("Step 5: Verify MDR operator survived OCP upgrade")

				Eventually(func() error {
					_, csvErr := mdrutils.FindSucceededCSV(
						APIClient, medik8sparams.OperatorPackage)

					return csvErr
				}, medik8sparams.PostUpgradeRecoveryTimeout, mdrparams.DefaultPollInterval).Should(Succeed(),
					"MDR CSV not in Succeeded phase after OCP upgrade")

				mdrDeploy, err = deployment.Pull(
					APIClient, mdrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
				Expect(err).NotTo(HaveOccurred())
				Expect(mdrDeploy.IsReady(medik8sparams.PostUpgradeRecoveryTimeout)).To(BeTrue(),
					"MDR deployment not Ready after OCP upgrade")

				By("Step 6: Validate GA MDR remediation on OCP N")

				targetWorkerName, err = runMDRRemediationCycle(ctx, initialWorkerCount,
					initialWorkerNames, "post-ocp-upgrade")
				Expect(err).NotTo(HaveOccurred(),
					"Post-OCP-upgrade remediation failed with GA operator")

				delete(initialWorkerNames, targetWorkerName)

				By("Cleaning up MDR CR from post-OCP-upgrade remediation")
				cleanupMDRCR(targetWorkerName)
				initialWorkerNames[targetWorkerName] = true

				By("Step 7: Apply deferred IDMS for Konflux catalog images")

				Expect(medik8sparams.SharedDir).NotTo(BeEmpty(),
					"SHARED_DIR must be set (provided by ci-operator)")

				preIDMSGens, genErr := helpers.GetMCPGenerations(ctx)
				Expect(genErr).NotTo(HaveOccurred(),
					"Failed to capture MCP generations before IDMS apply")

				idmsChanged, applyErr := helpers.ApplyIDMSFromSharedDir(ctx,
					medik8sparams.SharedDir, GinkgoWriter.Printf)
				Expect(applyErr).NotTo(HaveOccurred(),
					"Failed to apply IDMS from SHARED_DIR")

				if idmsChanged {
					By("Waiting for MCP rollout after IDMS change")

					Expect(helpers.WaitForMCPRollout(ctx, preIDMSGens,
						medik8sparams.MCPDetectionTimeout,
						medik8sparams.MCPRolloutTimeout,
						10*time.Second, GinkgoWriter.Printf,
					)).To(Succeed(), "MCP rollout failed after IDMS apply")
				} else {
					GinkgoWriter.Println("IDMS unchanged, skipping MCP rollout wait")
				}

				By("Step 8: Switch operator Subscription to Konflux CatalogSource")

				_, err = mdrutils.SwitchSubscriptionCatalog(
					APIClient, medik8sparams.UpgradeCatalogName)
				Expect(err).NotTo(HaveOccurred(),
					"Failed to switch Subscription to target catalog")

				By("Step 9: Wait for new CSV to reach Succeeded")

				var operatorUpgraded bool

				Eventually(func() error {
					csvs, listErr := olm.ListClusterServiceVersionWithNamePattern(
						APIClient, medik8sparams.OperatorPackage, medik8sparams.OperatorNs)
					if listErr != nil {
						return listErr
					}

					for _, csv := range csvs {
						csvPhase, _ := csv.GetPhase()
						if csvPhase != olmV1alpha1.CSVPhaseSucceeded {
							continue
						}

						if csv.Object.Name != previousCSV.Object.Name {
							GinkgoWriter.Printf("New CSV: %s (was: %s)\n",
								csv.Object.Name, previousCSV.Object.Name)

							operatorUpgraded = true
						} else {
							GinkgoWriter.Printf(
								"Version parity: Konflux catalog offers same version %s as GA\n",
								csv.Object.Name)
						}

						return nil
					}

					return fmt.Errorf("MDR CSV not yet in Succeeded phase after catalog switch")
				}, medik8sparams.OperatorUpgradeTimeout, mdrparams.DefaultPollInterval).Should(Succeed(),
					"Operator upgrade or catalog switch verification failed")

				if operatorUpgraded {
					By("Step 10: Verify MDR controller pods restarted with new image")

					Eventually(func() error {
						currentImage, imgErr := mdrutils.GetMDRControllerImage(APIClient)
						if imgErr != nil {
							return imgErr
						}

						if currentImage == preUpgradeImage {
							return fmt.Errorf("controller still running old image %s", preUpgradeImage)
						}

						GinkgoWriter.Printf("Controller image updated: %s\n", currentImage)

						return nil
					}, medik8sparams.OperatorUpgradeTimeout,
						mdrparams.DefaultPollInterval).Should(Succeed(),
						"MDR controller pods did not restart with new image")
				} else {
					GinkgoWriter.Println(
						"Step 10: Skipped (no operator upgrade occurred, " +
							"Konflux and GA catalogs at same version)")
				}

				By("Step 11: Validate MDR on OCP N (post-catalog-switch remediation)")

				targetWorkerName, err = runMDRRemediationCycle(ctx, initialWorkerCount,
					initialWorkerNames, "post-catalog-switch")
				Expect(err).NotTo(HaveOccurred(),
					"Post-catalog-switch remediation failed")
			})
	})

// upgradeOCP triggers an OCP cluster upgrade and waits for completion.
func upgradeOCP(ctx context.Context, targetImage string) {
	clusterVersion := &configv1.ClusterVersion{}
	Expect(APIClient.Get(ctx, client.ObjectKey{Name: "version"}, clusterVersion)).
		To(Succeed(), "Failed to get ClusterVersion")

	clusterVersion.Spec.DesiredUpdate = &configv1.Update{
		Image: targetImage,
		Force: true, // CI release images lack signed update graph metadata
	}

	Expect(APIClient.Update(ctx, clusterVersion)).
		To(Succeed(), "Failed to set desired OCP update")

	GinkgoWriter.Printf("OCP upgrade initiated to image: %s\n", targetImage)

	Expect(helpers.WaitForClusterVersionCondition(ctx, APIClient,
		"Progressing", configv1.ConditionTrue,
		medik8sparams.OCPUpgradeStartTimeout, mdrparams.DefaultPollInterval,
	)).To(Succeed(), "OCP upgrade did not start progressing")

	Expect(helpers.WaitForClusterVersionCondition(ctx, APIClient,
		"Progressing", configv1.ConditionFalse,
		medik8sparams.OCPUpgradeTimeout, mdrparams.DefaultPollInterval,
	)).To(Succeed(), "OCP upgrade did not complete (still Progressing)")

	Expect(helpers.WaitForClusterVersionCondition(ctx, APIClient,
		"Available", configv1.ConditionTrue,
		medik8sparams.PostUpgradeRecoveryTimeout, mdrparams.DefaultPollInterval,
	)).To(Succeed(), "Cluster not Available after OCP upgrade")

	Expect(helpers.WaitForClusterVersionCondition(ctx, APIClient,
		"Failing", configv1.ConditionFalse,
		medik8sparams.PostUpgradeRecoveryTimeout, mdrparams.DefaultPollInterval,
	)).To(Succeed(), "Cluster is Failing after OCP upgrade")

	GinkgoWriter.Println("OCP upgrade completed and cluster is healthy")
}

// runMDRRemediationCycle selects a worker node not running the MDR controller,
// stops its kubelet to trigger an NHC-driven MDR remediation, and waits for
// the Machine to be deleted and a replacement node to join Ready.
// Returns the name of the node that was targeted (the caller is responsible
// for tracking the replacement in initialWorkerNames for subsequent cycles).
func runMDRRemediationCycle(
	ctx context.Context, expectedWorkerCount int,
	initialWorkerNames map[string]bool, phase string,
) (string, error) {
	controllerNodes, err := getMDRControllerNodes(ctx)
	if err != nil {
		return "", fmt.Errorf("[%s] failed to get controller nodes: %w", phase, err)
	}

	selectedNode, err := helpers.SelectWorkerNode(ctx, APIClient, controllerNodes...)
	if err != nil {
		return "", fmt.Errorf("[%s] failed to select target node: %w", phase, err)
	}

	nodeName := selectedNode.Name
	GinkgoWriter.Printf("[%s] Target node: %s (excluding controller nodes: %v)\n",
		phase, nodeName, controllerNodes)

	testStartTime := time.Now()

	By(fmt.Sprintf("[%s] Stopping kubelet on worker node %s", phase, nodeName))

	if stopErr := stopKubeletForRemediation(ctx, nodeName); stopErr != nil {
		return "", fmt.Errorf("[%s] failed to stop kubelet on %s: %w", phase, nodeName, stopErr)
	}

	By(fmt.Sprintf("[%s] Waiting for MDR CR to be created by NHC", phase))

	Eventually(func() error {
		_, condErr := getMDRCRCondition(nodeName, mdrparams.ProcessingConditionType)

		return condErr
	}, mdrparams.NodeNotReadyTimeout, mdrparams.DefaultPollInterval).Should(Succeed(),
		"[%s] MDR CR with Processing condition not found for node %s", phase, nodeName)

	By(fmt.Sprintf("[%s] Waiting for MDR remediation to complete", phase))

	newNodeName, waitErr := waitForMDRRemediationComplete(
		ctx, nodeName, expectedWorkerCount, initialWorkerNames,
		testStartTime, mdrparams.RemediationCompleteTimeout,
	)
	if waitErr != nil {
		return nodeName, fmt.Errorf("[%s] MDR remediation did not complete for node %s: %w",
			phase, nodeName, waitErr)
	}

	By(fmt.Sprintf("[%s] Waiting for replacement node %s to become Ready", phase, newNodeName))

	if readyErr := helpers.WaitForNodeReady(
		ctx, APIClient, newNodeName,
		mdrparams.DefaultPollInterval, mdrparams.NodeReadyTimeout,
		GinkgoWriter.Printf,
	); readyErr != nil {
		return newNodeName, fmt.Errorf("[%s] replacement node %s did not become Ready: %w",
			phase, newNodeName, readyErr)
	}

	GinkgoWriter.Printf("[%s] Remediation cycle completed, replacement node: %s\n",
		phase, newNodeName)

	return newNodeName, nil
}

// getMDRControllerNodes returns the node names where MDR controller-manager
// pods are running, so remediation cycles can exclude them from targets.
func getMDRControllerNodes(ctx context.Context) ([]string, error) {
	_ = ctx

	listOptions := metav1.ListOptions{
		LabelSelector: mdrparams.OperatorControllerPodLabelSelector,
	}

	controllerPods, err := pod.List(APIClient, medik8sparams.OperatorNs, listOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list MDR controller pods: %w", err)
	}

	runningPods := helpers.FilterRunningPods(controllerPods)
	nodes := make([]string, 0, len(runningPods))

	for _, p := range runningPods {
		if p.Object.Spec.NodeName != "" {
			nodes = append(nodes, p.Object.Spec.NodeName)
		}
	}

	return nodes, nil
}
