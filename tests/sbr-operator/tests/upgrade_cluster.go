package tests

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrutils"
)

// This spec exercises only the OCP N-1 -> N cluster upgrade path; the
// operator/catalog upgrade path is exercised independently by
// "SBR Operator Upgrade" in upgrade_operator.go. Each spec is fully
// self-contained (its own fresh GA install). If MEDIK8S_SKIP_OCP_UPGRADE=true,
// this whole spec is skipped -- there's nothing left to test if the one thing
// it exists for doesn't run.
var _ = Describe("SBR Cluster Upgrade",
	Serial, Ordered,
	Label(labels.OperatorSBR, sbrparams.Label,
		labels.TierUpgradeCluster, labels.DisruptionDestructive,
		labels.PlatformAny, labels.FrequencyWeekly,
		labels.ComponentOLM),
	func() {
		var ctx context.Context

		BeforeAll(func() {
			ctx = context.Background()

			if medik8sparams.SkipOCPUpgrade {
				Skip("MEDIK8S_SKIP_OCP_UPGRADE=true: nothing to test in the cluster-upgrade-only spec")
			}

			Expect(medik8sparams.TargetOCPImage).NotTo(BeEmpty(),
				"OPENSHIFT_UPGRADE_RELEASE_IMAGE_OVERRIDE or RELEASE_IMAGE_LATEST must be set")
		})

		AfterAll(func() {
			cleanupUpgradeSBRC()
			sbrutils.CleanupUpgradeResources(APIClient, GinkgoWriter.Printf)
		})

		JustAfterEach(func() {
			sbrUpgradeFailureDump(ctx)
		})

		It("should survive OCP upgrade with a working agent DaemonSet",
			Label(labels.ComponentRemediation),
			func() {
				By("Step 1: Install SBR operator GA version from redhat-operators")

				_, err := sbrutils.InstallGAOperator(APIClient)
				Expect(err).NotTo(HaveOccurred(), "Failed to install GA SBR operator")

				By("Step 2: Wait for CSV to reach Succeeded and deployment to be Ready")

				var previousCSV *olm.ClusterServiceVersionBuilder

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

				GinkgoWriter.Printf("GA SBR CSV: %s\n", previousCSV.Object.Name)

				By("Step 3: Validate GA SBR agent DaemonSet on OCP N-1")

				runSBRCFunctionCheck("pre-ocp-upgrade")

				By("Step 4: Upgrade OCP from N-1 to N")

				upgradeOCP(ctx, medik8sparams.TargetOCPImage)

				By("Step 5: Verify SBR operator survived OCP upgrade")

				Eventually(func() error {
					_, csvErr := sbrutils.FindSucceededCSV(
						APIClient, medik8sparams.OperatorPackage)

					return csvErr
				}, medik8sparams.PostUpgradeRecoveryTimeout, sbrparams.DefaultPollInterval).Should(Succeed(),
					"SBR CSV not in Succeeded phase after OCP upgrade")

				sbrDeploy, err = deployment.Pull(
					APIClient, sbrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
				Expect(err).NotTo(HaveOccurred())
				Expect(sbrDeploy.IsReady(medik8sparams.PostUpgradeRecoveryTimeout)).To(BeTrue(),
					"SBR deployment not Ready after OCP upgrade")

				By("Step 6: Validate SBR agent DaemonSet on OCP N")

				runSBRCFunctionCheck("post-ocp-upgrade")
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
		medik8sparams.OCPUpgradeStartTimeout, sbrparams.DefaultPollInterval,
	)).To(Succeed(), "OCP upgrade did not start progressing")

	Expect(helpers.WaitForClusterVersionCondition(ctx, APIClient,
		"Progressing", configv1.ConditionFalse,
		medik8sparams.OCPUpgradeTimeout, sbrparams.DefaultPollInterval,
	)).To(Succeed(), "OCP upgrade did not complete (still Progressing)")

	Expect(helpers.WaitForClusterVersionCondition(ctx, APIClient,
		"Available", configv1.ConditionTrue,
		medik8sparams.PostUpgradeRecoveryTimeout, sbrparams.DefaultPollInterval,
	)).To(Succeed(), "Cluster not Available after OCP upgrade")

	Expect(helpers.WaitForClusterVersionCondition(ctx, APIClient,
		"Failing", configv1.ConditionFalse,
		medik8sparams.PostUpgradeRecoveryTimeout, sbrparams.DefaultPollInterval,
	)).To(Succeed(), "Cluster is Failing after OCP upgrade")

	GinkgoWriter.Println("OCP upgrade completed and cluster is healthy")
}
