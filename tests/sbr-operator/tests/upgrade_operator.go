package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"
	olmV1alpha1 "github.com/rh-ecosystem-edge/eco-goinfra/pkg/schemes/olm/operators/v1alpha1"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrutils"
)

// SBR's full remediation path needs ODF/CephFS storage, watchdog devices, and
// NHC-triggered fault injection (see nhc_integration.go). Exercising that
// whole chain inside an upgrade test would make it both slower and more
// infra-fragile than it needs to be. Instead this spec uses the simplest real
// proof that the operator is functioning end to end: creating a
// StorageBasedRemediationConfig and confirming its agent DaemonSet reaches
// Ready (reusing buildSBRC/waitForSBRCReady from sbr.go). This spec exercises
// only the operator/catalog upgrade path (GA -> Konflux pre-GA catalog); the
// OCP N-1 -> N cluster upgrade path is exercised independently by
// "SBR Cluster Upgrade" in upgrade_cluster.go. Each spec is fully
// self-contained (its own fresh GA install).
var _ = Describe("SBR Operator Upgrade",
	Serial, Ordered,
	Label(labels.OperatorSBR, sbrparams.Label,
		labels.TierUpgradeOperator, labels.DisruptionDestructive,
		labels.PlatformAny, labels.FrequencyWeekly,
		labels.ComponentOLM),
	func() {
		var (
			ctx             context.Context
			previousCSV     *olm.ClusterServiceVersionBuilder
			preUpgradeImage string
		)

		BeforeAll(func() {
			ctx = context.Background()
		})

		AfterAll(func() {
			cleanupUpgradeSBRC()
			sbrutils.CleanupUpgradeResources(APIClient, GinkgoWriter.Printf)
		})

		JustAfterEach(func() {
			sbrUpgradeFailureDump(ctx)
		})

		It("should survive operator upgrade with a working agent DaemonSet",
			Label(labels.ComponentRemediation),
			func() {
				By("Step 1: Install SBR operator GA version from redhat-operators")

				_, err := sbrutils.InstallGAOperator(APIClient)
				Expect(err).NotTo(HaveOccurred(), "Failed to install GA SBR operator")

				By("Step 2: Wait for CSV to reach Succeeded and deployment to be Ready")

				Eventually(func() error {
					var csvErr error
					previousCSV, csvErr = sbrutils.FindSucceededCSV(
						APIClient, medik8sparams.OperatorPackage)

					return csvErr
				}, medik8sparams.OperatorUpgradeTimeout, sbrparams.DefaultPollInterval).Should(Succeed(),
					"SBR CSV must reach Succeeded phase")

				sbrDeploy, err := deployment.Pull(
					APIClient, sbrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
				Expect(err).NotTo(HaveOccurred(), "Failed to get SBR deployment")
				Expect(sbrDeploy.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
					"SBR deployment is not Ready")

				By("Step 3: Record GA install checkpoint")

				preUpgradeImage, err = sbrutils.GetSBRControllerImage(APIClient)
				Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("GA operator image: %s\n", preUpgradeImage)
				GinkgoWriter.Printf("GA SBR CSV: %s\n", previousCSV.Object.Name)

				By("Step 4: Validate GA SBR agent DaemonSet baseline")

				runSBRCFunctionCheck("pre-catalog-switch")

				By("Step 5: Apply deferred IDMS for Konflux catalog images")

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

				By("Step 6: Switch operator Subscription to Konflux CatalogSource")

				_, err = sbrutils.SwitchSubscriptionCatalog(
					APIClient, medik8sparams.UpgradeCatalogName)
				Expect(err).NotTo(HaveOccurred(),
					"Failed to switch Subscription to target catalog")

				By("Step 7: Wait for new CSV to reach Succeeded")

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

					return fmt.Errorf("SBR CSV not yet in Succeeded phase after catalog switch")
				}, medik8sparams.OperatorUpgradeTimeout, sbrparams.DefaultPollInterval).Should(Succeed(),
					"Operator upgrade or catalog switch verification failed")

				if operatorUpgraded {
					By("Step 8: Verify SBR controller pods restarted with new image")

					Eventually(func() error {
						currentImage, imgErr := sbrutils.GetSBRControllerImage(APIClient)
						if imgErr != nil {
							return imgErr
						}

						if currentImage == preUpgradeImage {
							return fmt.Errorf("controller still running old image %s", preUpgradeImage)
						}

						GinkgoWriter.Printf("Controller image updated: %s\n", currentImage)

						return nil
					}, medik8sparams.OperatorUpgradeTimeout,
						sbrparams.DefaultPollInterval).Should(Succeed(),
						"SBR controller pods did not restart with new image")
				} else {
					GinkgoWriter.Println(
						"Step 8: Skipped (no operator upgrade occurred, " +
							"Konflux and GA catalogs at same version)")
				}

				By("Step 9: Validate SBR agent DaemonSet on upgraded operator (post-catalog-switch)")

				runSBRCFunctionCheck("post-catalog-switch")
			})
	})
