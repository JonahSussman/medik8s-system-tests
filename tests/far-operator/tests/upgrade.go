package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"
	olmV1alpha1 "github.com/rh-ecosystem-edge/eco-goinfra/pkg/schemes/olm/operators/v1alpha1"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
	"github.com/medik8s/system-tests/tests/far-operator/internal/farutils"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
)

var _ = Describe("FAR Operator Upgrade",
	Serial, Ordered,
	Label(labels.OperatorFAR, farparams.Label,
		labels.TierUpgrade, labels.DisruptionDestructive,
		labels.PlatformAWS, labels.FrequencyWeekly,
		labels.ComponentOLM),
	func() {
		var (
			ctx             context.Context
			previousCSV     *olm.ClusterServiceVersionBuilder
			preUpgradeImage string
			platform        configv1.PlatformType
			region          string
			fenceAgent      string
			sharedParams    map[string]interface{}
			nodeParams      map[string]interface{}
			leaderNode      string
			currentFARName  string
		)

		BeforeAll(func() {
			ctx = context.Background()

			Expect(farparams.TargetOCPImage).NotTo(BeEmpty(),
				"OPENSHIFT_UPGRADE_RELEASE_IMAGE_OVERRIDE or RELEASE_IMAGE_LATEST must be set")

			By("Detecting cluster platform")

			var err error

			platform, region, err = helpers.DetectPlatform(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform).To(Equal(configv1.AWSPlatformType),
				"Upgrade tests require AWS for fence agent remediation")

			By("Verifying at least 3 Ready worker nodes")

			workerCount, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(workerCount).To(BeNumerically(">=", 3),
				"Upgrade tests require at least 3 Ready worker nodes")

			By("Creating shared credentials Secret for remediation")

			awsAccessKey, awsSecretKey, credErr := farutils.GetAWSCredentials(
				ctx, APIClient, medik8sparams.OperatorNs)
			Expect(credErr).ToNot(HaveOccurred(), "Failed to get AWS credentials")

			credentialsSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      farparams.SharedCredentialsSecretName,
					Namespace: medik8sparams.OperatorNs,
				},
				StringData: map[string]string{
					"--access-key": awsAccessKey,
					"--secret-key": awsSecretKey,
				},
			}

			Expect(APIClient.Create(ctx, credentialsSecret)).
				To(Succeed(), "Failed to create credentials Secret")

			DeferCleanup(func() {
				if delErr := APIClient.Delete(ctx, credentialsSecret); delErr != nil &&
					!k8serrors.IsNotFound(delErr) {
					GinkgoWriter.Printf("WARNING: failed to delete credentials Secret: %v\n", delErr)
				}
			})
		})

		AfterAll(func() {
			farutils.CleanupUpgradeResources(APIClient, GinkgoWriter.Printf)
		})

		JustAfterEach(func() {
			specReport := CurrentSpecReport()
			if specReport.Failed() {
				GinkgoWriter.Println("Upgrade test failed - collecting FAR controller logs")
				logFARControllerState(ctx, APIClient)
			}

			if currentFARName != "" {
				farNodeName := currentFARName

				By("Waiting for FAR CR to reach Succeeded before cleanup")

				pollCtx, pollCancel := context.WithTimeout(ctx, farparams.FARConditionTimeout)
				defer pollCancel()

				if waitErr := wait.PollUntilContextCancel(pollCtx, farparams.DefaultPollInterval, true,
					func(pollCtx context.Context) (bool, error) {
						farObj := &unstructured.Unstructured{}
						farObj.SetGroupVersionKind(farGVK)

						if err := APIClient.Get(pollCtx, client.ObjectKey{
							Name:      currentFARName,
							Namespace: medik8sparams.OperatorNs,
						}, farObj); err != nil {
							return false, nil
						}

						conditions, found, nestedErr := unstructured.NestedSlice(
							farObj.Object, "status", "conditions")
						if nestedErr != nil {
							GinkgoWriter.Printf("WARNING: failed to read FAR conditions: %v\n", nestedErr)
							return false, nil
						}
						if !found {
							return false, nil
						}

						for _, c := range conditions {
							condMap, ok := c.(map[string]interface{})
							if !ok {
								continue
							}

							if condMap["type"] == farparams.FARConditionSucceeded &&
								condMap["status"] == string(metav1.ConditionTrue) {
								return true, nil
							}
						}

						return false, nil
					}); waitErr != nil {
					GinkgoWriter.Printf(
						"WARNING: FAR CR %s did not reach Succeeded within %s: %v\n",
						currentFARName, farparams.FARConditionTimeout, waitErr)
				}

				By("Deleting FAR CR " + currentFARName)
				deleteRemediationCR(ctx, APIClient, farGVK, currentFARName)
				currentFARName = ""

				By("Verifying FAR NoSchedule taint removed after CR deletion")

				taintCtx, taintCancel := context.WithTimeout(ctx, farparams.FARConditionTimeout)
				defer taintCancel()

				if taintErr := wait.PollUntilContextCancel(taintCtx, farparams.DefaultPollInterval, true,
					func(pollCtx context.Context) (bool, error) {
						node := &corev1.Node{}
						if err := APIClient.Get(pollCtx, client.ObjectKey{Name: farNodeName}, node); err != nil {
							return false, nil
						}

						for _, taint := range node.Spec.Taints {
							if taint.Key == farparams.FARNoScheduleTaintKey {
								return false, nil
							}
						}

						return true, nil
					}); taintErr != nil {
					GinkgoWriter.Printf(
						"WARNING: FAR taint still present on node %s after %s: %v\n",
						farNodeName, farparams.FARConditionTimeout, taintErr)
				}

				By("Safety net: waiting for node " + farNodeName + " to become Ready")

				if err := farutils.WaitForNodeReady(
					ctx, APIClient, farNodeName, farparams.NodeReadyTimeout); err != nil {
					GinkgoWriter.Printf(
						"WARNING: node %s did not become Ready within %s: %v\n",
						farNodeName, farparams.NodeReadyTimeout, err)
					AddReportEntry("upgrade-recovery-failed",
						fmt.Sprintf("node %s did not recover: %v", farNodeName, err))
				}
			}
		})

		It("should survive OCP upgrade and operator upgrade with working remediation",
			Label(labels.ComponentRemediation),
			reportxml.ID("OCP-89717"),
			func() {
				By("Step 1: Install FAR operator GA version from redhat-operators on OCP N-1")

				_, err := farutils.InstallGAOperator(APIClient)
				Expect(err).NotTo(HaveOccurred(), "Failed to install GA FAR operator")

				By("Step 2: Deploy FAR controller and verify it is running")

				Eventually(func() error {
					var csvErr error

					previousCSV, csvErr = farutils.FindSucceededCSV(
						APIClient, farparams.OperatorPackage)

					return csvErr
				}, farparams.OperatorUpgradeTimeout, farparams.DefaultPollInterval).Should(Succeed(),
					"FAR CSV must reach Succeeded phase on OCP N-1")

				farDeploy, err := deployment.Pull(
					APIClient, farparams.OperatorDeploymentName, medik8sparams.OperatorNs)
				Expect(err).NotTo(HaveOccurred(), "Failed to get FAR deployment")
				Expect(farDeploy.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
					"FAR deployment is not Ready on OCP N-1")

				preUpgradeImage, err = farutils.GetFARControllerImage(APIClient)
				Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("GA operator image: %s\n", preUpgradeImage)

				By("Step 3: Verify GA FAR installation on OCP N-1 (install checkpoint)")

				Expect(previousCSV).NotTo(BeNil(), "No FAR CSV in Succeeded phase")
				GinkgoWriter.Printf("GA FAR CSV: %s\n", previousCSV.Object.Name)

				By("Step 4: Upgrade OCP from N-1 to N")

				clusterVersion := &configv1.ClusterVersion{}
				Expect(APIClient.Get(ctx, client.ObjectKey{Name: "version"}, clusterVersion)).
					To(Succeed(), "Failed to get ClusterVersion")

				clusterVersion.Spec.DesiredUpdate = &configv1.Update{
					Image: farparams.TargetOCPImage,
					Force: true,
				}

				Expect(APIClient.Update(ctx, clusterVersion)).
					To(Succeed(), "Failed to set desired OCP update")

				GinkgoWriter.Printf("OCP upgrade initiated to image: %s\n",
					farparams.TargetOCPImage)

				waitForClusterVersionCondition(ctx,
					"Progressing", configv1.ConditionTrue,
					farparams.OCPUpgradeStartTimeout,
					"OCP upgrade did not start progressing")

				waitForClusterVersionCondition(ctx,
					"Progressing", configv1.ConditionFalse,
					farparams.OCPUpgradeTimeout,
					"OCP upgrade did not complete (still Progressing)")

				waitForClusterVersionCondition(ctx,
					"Available", configv1.ConditionTrue,
					farparams.PostUpgradeRecoveryTimeout,
					"Cluster not Available after OCP upgrade")

				waitForClusterVersionCondition(ctx,
					"Degraded", configv1.ConditionFalse,
					farparams.PostUpgradeRecoveryTimeout,
					"Cluster is Degraded after OCP upgrade")

				GinkgoWriter.Println("OCP upgrade completed and cluster is healthy")

				By("Step 5: Verify FAR operator pod survived OCP upgrade and CSV is Succeeded")

				Eventually(func() error {
					_, csvErr := farutils.FindSucceededCSV(
						APIClient, farparams.OperatorPackage)

					return csvErr
				}, farparams.PostUpgradeRecoveryTimeout, farparams.DefaultPollInterval).Should(Succeed(),
					"FAR CSV not in Succeeded phase after OCP upgrade")

				farDeploy, err = deployment.Pull(
					APIClient, farparams.OperatorDeploymentName, medik8sparams.OperatorNs)
				Expect(err).NotTo(HaveOccurred())
				Expect(farDeploy.IsReady(farparams.PostUpgradeRecoveryTimeout)).To(BeTrue(),
					"FAR deployment not Ready after OCP upgrade")

				By("Step 6: Validate GA FAR on OCP N (post-OCP-upgrade remediation)")

				fenceAgent, sharedParams, nodeParams, leaderNode, err =
					upgradeProvisionRemediationResources(ctx, platform, region)
				Expect(err).NotTo(HaveOccurred(), "Failed to set up remediation resources")

				currentFARName, err = upgradeRunRemediationCycle(
					ctx, fenceAgent, sharedParams, nodeParams, leaderNode, "post-ocp-upgrade")
				Expect(err).NotTo(HaveOccurred(),
					"Post-OCP-upgrade remediation failed with GA operator")

				By("Cleaning up FAR CR from post-OCP-upgrade remediation")
				deleteRemediationCR(ctx, APIClient, farGVK, currentFARName)
				currentFARName = ""

				By("Step 7: Switch operator Subscription to Konflux CatalogSource")

				_, err = farutils.SwitchSubscriptionCatalog(
					APIClient, farparams.UpgradeCatalogName)
				Expect(err).NotTo(HaveOccurred(),
					"Failed to switch Subscription to target catalog")

				By("Step 8: Wait for new CSV and verify it reached Succeeded")

				Eventually(func() error {
					csvs, listErr := olm.ListClusterServiceVersionWithNamePattern(
						APIClient, farparams.OperatorPackage, medik8sparams.OperatorNs)
					if listErr != nil {
						return listErr
					}

					for _, csv := range csvs {
						csvPhase, _ := csv.GetPhase()
						if csvPhase == olmV1alpha1.CSVPhaseSucceeded &&
							csv.Object.Name != previousCSV.Object.Name {
							GinkgoWriter.Printf("New CSV: %s (was: %s)\n",
								csv.Object.Name, previousCSV.Object.Name)

							return nil
						}
					}

					return fmt.Errorf("new FAR CSV not yet in Succeeded phase")
				}, farparams.OperatorUpgradeTimeout, farparams.DefaultPollInterval).Should(Succeed(),
					"Operator upgrade did not complete")

				By("Step 9: Verify FAR controller pods restarted with new image")

				Eventually(func() error {
					currentImage, imgErr := farutils.GetFARControllerImage(APIClient)
					if imgErr != nil {
						return imgErr
					}

					if currentImage == preUpgradeImage {
						return fmt.Errorf("controller still running old image %s", preUpgradeImage)
					}

					GinkgoWriter.Printf("Controller image updated: %s\n", currentImage)

					return nil
				}, farparams.OperatorUpgradeTimeout, farparams.DefaultPollInterval).Should(Succeed(),
					"FAR controller pods did not restart with new image")

				By("Step 10: Validate pre-GA FAR on OCP N (post-operator-upgrade remediation)")

				fenceAgent, sharedParams, nodeParams, leaderNode, err =
					upgradeProvisionRemediationResources(ctx, platform, region)
				Expect(err).NotTo(HaveOccurred(), "Failed to set up remediation resources")

				currentFARName, err = upgradeRunRemediationCycle(
					ctx, fenceAgent, sharedParams, nodeParams, leaderNode, "post-operator-upgrade")
				Expect(err).NotTo(HaveOccurred(),
					"Post-operator-upgrade remediation failed with pre-GA operator")

				By("Cleaning up FAR CR from post-operator-upgrade remediation")
				deleteRemediationCR(ctx, APIClient, farGVK, currentFARName)
				currentFARName = ""
			})
	})

func waitForClusterVersionCondition(
	ctx context.Context,
	condType string, condStatus configv1.ConditionStatus,
	timeout time.Duration, failureMsg string,
) {
	Eventually(func() bool {
		clsVer := &configv1.ClusterVersion{}
		if getErr := APIClient.Get(ctx, client.ObjectKey{Name: "version"}, clsVer); getErr != nil {
			return false
		}

		for _, cond := range clsVer.Status.Conditions {
			if string(cond.Type) == condType && cond.Status == condStatus {
				return true
			}
		}

		return false
	}, timeout, farparams.DefaultPollInterval).Should(BeTrue(), failureMsg)
}

// upgradeProvisionRemediationResources resolves the fence agent, builds node
// parameters, and waits for leader election to settle. The credentials Secret
// is created once in BeforeAll.
func upgradeProvisionRemediationResources(
	ctx context.Context,
	platform configv1.PlatformType,
	region string,
) (string, map[string]interface{}, map[string]interface{}, string, error) {
	fenceAgent, _, err := farutils.FenceAgentForPlatform(platform)
	if err != nil {
		return "", nil, nil, "", fmt.Errorf("failed to resolve fence agent: %w", err)
	}

	GinkgoWriter.Printf("Fence agent: %s, Region: %s\n", fenceAgent, region)

	sharedParams := map[string]interface{}{
		"--region":          region,
		"--action":          "reboot",
		"--skip-race-check": "",
	}

	awsNodeParams, err := farutils.BuildAWSNodeParameters(ctx, APIClient)
	if err != nil {
		return "", nil, nil, "", fmt.Errorf("failed to build AWS node parameters: %w", err)
	}

	nodeParams := make(map[string]interface{})

	for paramName, nodeMap := range awsNodeParams {
		inner := make(map[string]interface{}, len(nodeMap))
		for nodeName, val := range nodeMap {
			inner[nodeName] = val
		}

		nodeParams[paramName] = inner
	}

	var leader string

	Eventually(func() error {
		var leaderErr error

		leader, leaderErr = farutils.GetActiveFARControllerNode(ctx, APIClient)

		return leaderErr
	}, farparams.ControllerHandoverTimeout, farparams.DefaultPollInterval).Should(Succeed(),
		"FAR leader election did not settle")

	return fenceAgent, sharedParams, nodeParams, leader, nil
}

func upgradeRunRemediationCycle(
	ctx context.Context,
	fenceAgent string,
	sharedParams, nodeParams map[string]interface{},
	leaderNode, phase string,
) (string, error) {
	selectedNode, err := helpers.SelectWorkerNode(ctx, APIClient, leaderNode)
	if err != nil {
		return "", fmt.Errorf("[%s] failed to select target node: %w", phase, err)
	}

	nodeName := selectedNode.Name
	GinkgoWriter.Printf("[%s] Target node: %s (leader: %s)\n", phase, nodeName, leaderNode)

	originalBootID, err := farutils.GetNodeBootIDFromAPI(ctx, APIClient, nodeName)
	if err != nil {
		return "", fmt.Errorf("[%s] failed to get boot ID: %w", phase, err)
	}

	farCRName := nodeName

	By(fmt.Sprintf("[%s] Deleting any stale FAR CR from a prior run", phase))

	deleteRemediationCR(ctx, APIClient, farGVK, farCRName)

	By(fmt.Sprintf("[%s] Cleaning CRI-O overlay storage on %s", phase, nodeName))

	removeWorkloadImage(ctx, nodeName)

	workloadPodName := createWorkloadPodOnNode(ctx, nodeName, phase)

	GinkgoWriter.Printf("[%s] Workload pod %s running on %s\n",
		phase, workloadPodName, nodeName)

	farObj := buildFARUnstructured(farCRName, fenceAgent, sharedParams, nodeParams)

	GinkgoWriter.Printf("[%s] Creating FAR CR %s\n", phase, farCRName)

	Eventually(func() error {
		createErr := APIClient.Create(ctx, farObj)
		if createErr != nil && k8serrors.IsAlreadyExists(createErr) {
			GinkgoWriter.Printf("[%s] FAR CR %s already exists, treating as success\n",
				phase, farCRName)

			return nil
		}

		return createErr
	}, farparams.WorkloadPodReadyTimeout, farparams.DefaultPollInterval).Should(Succeed(),
		"[%s] Failed to create FAR CR %s", phase, farCRName)

	GinkgoWriter.Printf("[%s] Waiting for node %s remediation\n", phase, nodeName)

	waitForRemediation(ctx, APIClient, nodeName, originalBootID)

	By(fmt.Sprintf("[%s] Verifying workload pod %s was evicted", phase, workloadPodName))

	Eventually(func() bool {
		pod := &corev1.Pod{}
		getErr := APIClient.Get(ctx, client.ObjectKey{
			Name:      workloadPodName,
			Namespace: medik8sparams.OperatorNs,
		}, pod)

		return k8serrors.IsNotFound(getErr) || pod.DeletionTimestamp != nil
	}, farparams.WorkloadEvictionTimeout, farparams.DefaultPollInterval).Should(BeTrue(),
		"[%s] Workload pod %s was not evicted after remediation", phase, workloadPodName)

	GinkgoWriter.Printf("[%s] Remediation cycle completed for node %s (workload evicted)\n",
		phase, nodeName)

	return farCRName, nil
}

func createWorkloadPodOnNode(ctx context.Context, nodeName, phase string) string {
	By(fmt.Sprintf("[%s] Creating workload pod pinned to %s", phase, nodeName))

	workloadPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "far-upgrade-workload-",
			Namespace:    medik8sparams.OperatorNs,
		},
		Spec: corev1.PodSpec{
			NodeName:      nodeName,
			RestartPolicy: corev1.RestartPolicyAlways,
			Containers: []corev1.Container{{
				Name:    "workload",
				Image:   farparams.WorkloadTestImage,
				Command: []string{"sleep", "infinity"},
			}},
		},
	}

	Expect(APIClient.Create(ctx, workloadPod)).To(Succeed(),
		"[%s] Failed to create workload pod on %s", phase, nodeName)

	podName := workloadPod.Name

	DeferCleanup(func() {
		cleanupPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: medik8sparams.OperatorNs,
			},
		}

		if delErr := APIClient.Delete(ctx, cleanupPod); delErr != nil &&
			!k8serrors.IsNotFound(delErr) {
			GinkgoWriter.Printf("[%s] WARNING: failed to delete workload pod: %v\n",
				phase, delErr)
		}
	})

	Eventually(func() bool {
		pod := &corev1.Pod{}
		if getErr := APIClient.Get(ctx, client.ObjectKey{
			Name:      podName,
			Namespace: medik8sparams.OperatorNs,
		}, pod); getErr != nil {
			return false
		}

		if pod.Status.Phase != corev1.PodRunning {
			return false
		}

		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				return false
			}
		}

		return len(pod.Status.ContainerStatuses) == len(pod.Spec.Containers)
	}, farparams.WorkloadPodReadyTimeout, farparams.DefaultPollInterval).Should(BeTrue(),
		"[%s] Workload pod did not reach Running on %s", phase, nodeName)

	return podName
}
