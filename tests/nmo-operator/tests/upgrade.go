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
	olmV1alpha1 "github.com/rh-ecosystem-edge/eco-goinfra/pkg/schemes/olm/operators/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/nmo-operator/internal/nmoparams"
	"github.com/medik8s/system-tests/tests/nmo-operator/internal/nmoutils"
)

// NMO does not remediate -- it manages maintenance mode (cordon/drain). The
// upgrade-test analog to FAR/SNR/MDR's remediation cycle is a maintenance
// cycle: put a node under maintenance via a NodeMaintenance CR and verify it
// cordons, taints, and drains correctly, then release it. See
// nmo_helpers.go/nmo_lifecycle.go for the reused newNodeMaintenance,
// assertMaintenanceSucceeded, deleteNMBestEffort, and selectSchedulableWorker
// helpers -- this file adds no new NMO-typed imports of its own.
var _ = Describe("NMO Operator Upgrade",
	Serial, Ordered,
	Label(labels.OperatorNMO, nmoparams.Label,
		labels.TierUpgrade, labels.DisruptionDestructive,
		labels.PlatformAny, labels.FrequencyWeekly,
		labels.ComponentOLM),
	func() {
		var (
			ctx             context.Context
			previousCSV     *olm.ClusterServiceVersionBuilder
			preUpgradeImage string
			currentNMName   string
			currentNMNode   string
		)

		BeforeAll(func() {
			ctx = context.Background()

			Expect(medik8sparams.TargetOCPImage).NotTo(BeEmpty(),
				"OPENSHIFT_UPGRADE_RELEASE_IMAGE_OVERRIDE or RELEASE_IMAGE_LATEST must be set")
		})

		AfterAll(func() {
			nmoutils.CleanupUpgradeResources(APIClient, GinkgoWriter.Printf)
		})

		JustAfterEach(func() {
			if currentNMName != "" {
				name := currentNMName
				node := currentNMNode
				currentNMName = ""
				currentNMNode = ""

				By("Safety net: deleting NodeMaintenance " + name)
				deleteNMBestEffort(ctx, name)

				By("Safety net: waiting for node " + node + " to recover")
				waitForNodeRecoveryBestEffort(ctx, node)
			}
		})

		It("should survive OCP upgrade and operator upgrade with working maintenance",
			Label(labels.ComponentRemediation),
			func() {
				By("Step 1: Install NMO operator GA version from redhat-operators")

				_, err := nmoutils.InstallGAOperator(APIClient)
				Expect(err).NotTo(HaveOccurred(), "Failed to install GA NMO operator")

				By("Step 2: Wait for CSV to reach Succeeded and deployment to be Ready")

				Eventually(func() error {
					var csvErr error
					previousCSV, csvErr = nmoutils.FindSucceededCSV(
						APIClient, medik8sparams.OperatorPackage)

					return csvErr
				}, medik8sparams.OperatorUpgradeTimeout, nmoparams.DefaultPollInterval).Should(Succeed(),
					"NMO CSV must reach Succeeded phase")

				nmoDeploy, err := deployment.Pull(
					APIClient, nmoparams.OperatorDeploymentName, medik8sparams.OperatorNs)
				Expect(err).NotTo(HaveOccurred(), "Failed to get NMO deployment")
				Expect(nmoDeploy.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
					"NMO deployment is not Ready")

				By("Step 3: Record GA install checkpoint")

				preUpgradeImage, err = nmoutils.GetNMOControllerImage(APIClient)
				Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("GA operator image: %s\n", preUpgradeImage)
				GinkgoWriter.Printf("GA NMO CSV: %s\n", previousCSV.Object.Name)

				By("Step 4: Validate GA NMO maintenance cycle on OCP N-1")

				currentNMName, currentNMNode, err = runNMOMaintenanceCycle(ctx, "pre-ocp-upgrade")
				Expect(err).NotTo(HaveOccurred(), "Pre-OCP-upgrade maintenance cycle failed")

				By("Cleaning up NodeMaintenance from pre-OCP-upgrade cycle")
				endNMOMaintenanceCycle(ctx, currentNMName, currentNMNode)
				currentNMName, currentNMNode = "", ""

				By("Step 5: Upgrade OCP from N-1 to N")

				upgradeOCP(ctx, medik8sparams.TargetOCPImage)

				By("Step 6: Verify NMO operator survived OCP upgrade")

				Eventually(func() error {
					_, csvErr := nmoutils.FindSucceededCSV(
						APIClient, medik8sparams.OperatorPackage)

					return csvErr
				}, medik8sparams.PostUpgradeRecoveryTimeout, nmoparams.DefaultPollInterval).Should(Succeed(),
					"NMO CSV not in Succeeded phase after OCP upgrade")

				nmoDeploy, err = deployment.Pull(
					APIClient, nmoparams.OperatorDeploymentName, medik8sparams.OperatorNs)
				Expect(err).NotTo(HaveOccurred())
				Expect(nmoDeploy.IsReady(medik8sparams.PostUpgradeRecoveryTimeout)).To(BeTrue(),
					"NMO deployment not Ready after OCP upgrade")

				By("Step 7: Validate GA NMO maintenance cycle on OCP N")

				currentNMName, currentNMNode, err = runNMOMaintenanceCycle(ctx, "post-ocp-upgrade")
				Expect(err).NotTo(HaveOccurred(), "Post-OCP-upgrade maintenance cycle failed")

				By("Cleaning up NodeMaintenance from post-OCP-upgrade cycle")
				endNMOMaintenanceCycle(ctx, currentNMName, currentNMNode)
				currentNMName, currentNMNode = "", ""

				By("Step 8: Apply deferred IDMS for Konflux catalog images")

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

				By("Step 9: Switch operator Subscription to Konflux CatalogSource")

				_, err = nmoutils.SwitchSubscriptionCatalog(
					APIClient, medik8sparams.UpgradeCatalogName)
				Expect(err).NotTo(HaveOccurred(),
					"Failed to switch Subscription to target catalog")

				By("Step 10: Wait for new CSV to reach Succeeded")

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

					return fmt.Errorf("NMO CSV not yet in Succeeded phase after catalog switch")
				}, medik8sparams.OperatorUpgradeTimeout, nmoparams.DefaultPollInterval).Should(Succeed(),
					"Operator upgrade or catalog switch verification failed")

				if operatorUpgraded {
					By("Step 11: Verify NMO controller pods restarted with new image")

					Eventually(func() error {
						currentImage, imgErr := nmoutils.GetNMOControllerImage(APIClient)
						if imgErr != nil {
							return imgErr
						}

						if currentImage == preUpgradeImage {
							return fmt.Errorf("controller still running old image %s", preUpgradeImage)
						}

						GinkgoWriter.Printf("Controller image updated: %s\n", currentImage)

						return nil
					}, medik8sparams.OperatorUpgradeTimeout,
						nmoparams.DefaultPollInterval).Should(Succeed(),
						"NMO controller pods did not restart with new image")
				} else {
					GinkgoWriter.Println(
						"Step 11: Skipped (no operator upgrade occurred, " +
							"Konflux and GA catalogs at same version)")
				}

				By("Step 12: Validate NMO on OCP N (post-catalog-switch maintenance cycle)")

				currentNMName, currentNMNode, err = runNMOMaintenanceCycle(ctx, "post-catalog-switch")
				Expect(err).NotTo(HaveOccurred(), "Post-catalog-switch maintenance cycle failed")
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
		medik8sparams.OCPUpgradeStartTimeout, nmoparams.DefaultPollInterval,
	)).To(Succeed(), "OCP upgrade did not start progressing")

	Expect(helpers.WaitForClusterVersionCondition(ctx, APIClient,
		"Progressing", configv1.ConditionFalse,
		medik8sparams.OCPUpgradeTimeout, nmoparams.DefaultPollInterval,
	)).To(Succeed(), "OCP upgrade did not complete (still Progressing)")

	Expect(helpers.WaitForClusterVersionCondition(ctx, APIClient,
		"Available", configv1.ConditionTrue,
		medik8sparams.PostUpgradeRecoveryTimeout, nmoparams.DefaultPollInterval,
	)).To(Succeed(), "Cluster not Available after OCP upgrade")

	Expect(helpers.WaitForClusterVersionCondition(ctx, APIClient,
		"Failing", configv1.ConditionFalse,
		medik8sparams.PostUpgradeRecoveryTimeout, nmoparams.DefaultPollInterval,
	)).To(Succeed(), "Cluster is Failing after OCP upgrade")

	GinkgoWriter.Println("OCP upgrade completed and cluster is healthy")
}

// runNMOMaintenanceCycle selects a schedulable worker, puts it under maintenance
// via a NodeMaintenance CR, and verifies it cordons, taints, and drains
// correctly. Reuses the existing collision-test helpers (nmo_helpers.go) --
// newNodeMaintenance, assertMaintenanceSucceeded, selectSchedulableWorker --
// rather than duplicating them.
func runNMOMaintenanceCycle(ctx context.Context, phase string) (nmName, nodeName string, err error) {
	nodeName = selectSchedulableWorker(ctx)
	nmName = fmt.Sprintf("nmo-upgrade-%s", phase)

	GinkgoWriter.Printf("[%s] Target node: %s\n", phase, nodeName)

	By(fmt.Sprintf("[%s] Creating NodeMaintenance for %s", phase, nodeName))

	nm := newNodeMaintenance(nmName, nodeName)
	if createErr := APIClient.Create(ctx, nm); createErr != nil {
		return nmName, nodeName, fmt.Errorf("[%s] failed to create NodeMaintenance %s: %w",
			phase, nmName, createErr)
	}

	By(fmt.Sprintf("[%s] Waiting for maintenance to succeed on %s", phase, nodeName))

	assertMaintenanceSucceeded(ctx, nmName, nodeName)

	GinkgoWriter.Printf("[%s] Maintenance cycle succeeded for node %s\n", phase, nodeName)

	return nmName, nodeName, nil
}

// endNMOMaintenanceCycle deletes the NodeMaintenance CR and waits for the node
// to recover (uncordoned, untainted, Ready) before the next cycle begins.
func endNMOMaintenanceCycle(ctx context.Context, nmName, nodeName string) {
	deleteNMBestEffort(ctx, nmName)
	waitForNodeRecoveryBestEffort(ctx, nodeName)
}
