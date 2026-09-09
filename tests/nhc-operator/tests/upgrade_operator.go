package tests

import (
	"context"
	"time"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/nhc-operator/internal/nhcparams"
	"github.com/medik8s/system-tests/tests/nhc-operator/internal/nhcutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("NHC operator bundle upgrade", Serial, Ordered,
	Label(labels.OperatorNHC, nhcparams.Label, labels.TierUpgradeOperator,
		labels.DisruptionNonDestructive, labels.PlatformAny, labels.ComponentOLM), func() {
		var (
			ctx        context.Context
			inputs     nhcparams.UpgradeInputs
			oldCSV     *olm.ClusterServiceVersionBuilder
			configUID  string
			configSpec map[string]interface{}
		)

		BeforeAll(func() {
			ctx = context.Background()
			var err error
			inputs, err = nhcparams.LoadUpgradeInputs()
			Expect(err).NotTo(HaveOccurred())
			Expect(inputs.Namespace).To(Equal(medik8sparams.OperatorNs), "NHC uses its established operator namespace")
			clusterVersion := &configv1.ClusterVersion{}
			Expect(APIClient.Get(ctx, client.ObjectKey{Name: "version"}, clusterVersion)).To(Succeed())
			Expect(clusterVersion.Status.Desired.Version).To(HavePrefix("5.0."), "requires an OpenShift 5.0 cluster")
			AddReportEntry("nhc-upgrade-inputs", map[string]string{
				"candidateBundle": inputs.CandidateBundle, "candidateCommit": inputs.CandidateCommit, "candidateImage": inputs.CandidateImage,
				"candidateVersion": inputs.CandidateVersion, "namespace": inputs.Namespace, "oldBundle": inputs.OldBundle,
				"oldImage": inputs.OldImage, "oldVersion": inputs.OldVersion, "package": inputs.Package,
				"sdk": inputs.OperatorSDK, "snrBundle": inputs.SNRBundle, "systemTestsRevision": inputs.TestRevision,
			})
		})

		AfterAll(func() {
			cleanupNHCCR(ctx, nhcparams.NHCUpgradeTestName)
			cleanupSNRT(ctx, nhcparams.NHCUpgradeTemplateName)
			for _, packageName := range []string{inputs.Package, inputs.SNRPackage} {
				if packageName == "" {
					continue
				}
				output, err := nhcutils.CleanupBundle(ctx, inputs.OperatorSDK, inputs.Namespace, packageName)
				GinkgoWriter.Printf("operator-sdk cleanup %s output:\n%s\n", packageName, output)
				if err != nil {
					GinkgoWriter.Printf("WARNING: cleanup %s failed: %v\n", packageName, err)
				}
			}
		})

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				logNHCControllerState()
				helpers.LogOLMDiagnostics(ctx, APIClient, inputs.Namespace, "", GinkgoWriter.Printf)
			}
		})

		It("installs a pinned old bundle and upgrades its preserved configuration", reportxml.ID("REPLACE_WITH_POLARION_ID"), func() {
			By("rejecting leftover resources owned by this standalone scenario")
			assertUpgradeScenarioIsClean(ctx, inputs)
			By("installing the pinned SNR prerequisite and its remediation template")
			output, err := nhcutils.InstallBundle(ctx, inputs.OperatorSDK, inputs.Namespace, inputs.SNRBundle)
			GinkgoWriter.Printf("operator-sdk run bundle (SNR) output:\n%s\n", output)
			Expect(err).NotTo(HaveOccurred())
			Expect(APIClient.Create(ctx, buildSNRT(nhcparams.NHCUpgradeTemplateName))).To(Succeed())
			Expect(waitForSNRTemplate(ctx, nhcparams.NHCUpgradeTemplateName)).To(Succeed())
			By("installing the explicitly pinned older upstream NHC bundle")
			output, err = nhcutils.InstallBundle(ctx, inputs.OperatorSDK, inputs.Namespace, inputs.OldBundle)
			GinkgoWriter.Printf("operator-sdk run bundle (old NHC) output:\n%s\n", output)
			Expect(err).NotTo(HaveOccurred())
			oldCSV = waitForNHCUpgradeCSV(inputs, inputs.OldVersion, "old")
			oldImage, err := nhcutils.GetNHCControllerImage(APIClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(oldImage).To(Equal(inputs.OldImage))
			By("creating a safe, observable NodeHealthCheck configuration")
			nhc := buildNHCWithSNRT(nhcparams.NHCUpgradeTestName, nhcparams.NHCUpgradeTemplateName)
			Expect(APIClient.Create(ctx, nhc)).To(Succeed())
			Expect(waitForNHCPhase(ctx, nhcparams.NHCUpgradeTestName, nhcparams.NHCPhaseEnabled, medik8sparams.DefaultTimeout)).To(Succeed())
			configUID, configSpec = captureNHCConfiguration(ctx)
			By("upgrading in place to the explicitly supplied candidate bundle")
			output, err = nhcutils.UpgradeBundle(ctx, inputs.OperatorSDK, inputs.Namespace, inputs.CandidateBundle)
			GinkgoWriter.Printf("operator-sdk run bundle-upgrade output:\n%s\n", output)
			Expect(err).NotTo(HaveOccurred(), "the old operator must not be uninstalled before upgrade")
			By("requiring a new CSV and the candidate version and image")
			newCSV := waitForNHCUpgradeCSV(inputs, inputs.CandidateVersion, "candidate")
			Expect(newCSV.Object.Name).NotTo(Equal(oldCSV.Object.Name), "version parity is not an upgrade")
			candidateImage, err := nhcutils.GetNHCControllerImage(APIClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(candidateImage).To(Equal(inputs.CandidateImage))
			By("verifying the same configuration identity, specification, and reconciliation")
			uid, spec := captureNHCConfiguration(ctx)
			Expect(uid).To(Equal(configUID), "upgrade must preserve the existing NodeHealthCheck")
			Expect(spec).To(Equal(configSpec), "upgrade must preserve the NodeHealthCheck specification")
			Expect(waitForNHCPhase(ctx, nhcparams.NHCUpgradeTestName, nhcparams.NHCPhaseEnabled, medik8sparams.DefaultTimeout)).To(Succeed())
		})
	})

func assertUpgradeScenarioIsClean(ctx context.Context, inputs nhcparams.UpgradeInputs) {
	csvs, err := olm.ListClusterServiceVersionWithNamePattern(APIClient, nhcparams.CSVNamePattern, inputs.Namespace)
	Expect(err).NotTo(HaveOccurred())
	Expect(csvs).To(BeEmpty(), "a pre-existing NHC installation is not test-owned and must not be replaced")
	for _, object := range []*unstructured.Unstructured{upgradeNHC(), upgradeTemplate(inputs.Namespace)} {
		err := APIClient.Get(ctx, client.ObjectKeyFromObject(object), object)
		Expect(errors.IsNotFound(err)).To(BeTrue(), "leftover test-owned resource %s: %v", object.GetName(), err)
	}
}

func upgradeNHC() *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(nhcGVK)
	object.SetName(nhcparams.NHCUpgradeTestName)
	return object
}

func upgradeTemplate(namespace string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(snrtGVK)
	object.SetName(nhcparams.NHCUpgradeTemplateName)
	object.SetNamespace(namespace)
	return object
}

func waitForSNRTemplate(ctx context.Context, name string) error {
	return wait.PollUntilContextTimeout(ctx, nhcparams.DefaultPollInterval, medik8sparams.DefaultTimeout, true, func(context.Context) (bool, error) {
		object := upgradeTemplate(medik8sparams.OperatorNs)
		object.SetName(name)
		err := APIClient.Get(ctx, client.ObjectKeyFromObject(object), object)
		return err == nil, client.IgnoreNotFound(err)
	})
}

func waitForNHCUpgradeCSV(inputs nhcparams.UpgradeInputs, expectedVersion, phase string) *olm.ClusterServiceVersionBuilder {
	var found *olm.ClusterServiceVersionBuilder
	Eventually(func(g Gomega) {
		csv, err := helpers.FindSucceededCSV(APIClient, nhcparams.CSVNamePattern, inputs.Namespace)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(csv.Object.Spec.Version.String()).To(Equal(expectedVersion))
		controller, err := deployment.Pull(APIClient, nhcparams.OperatorDeploymentName, inputs.Namespace)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(controller.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue())
		found = csv
	}, 15*time.Minute, nhcparams.DefaultPollInterval).Should(Succeed(), "%s NHC CSV did not become ready", phase)
	GinkgoWriter.Printf("%s NHC CSV: %s version=%s\n", phase, found.Object.Name, found.Object.Spec.Version.String())
	return found
}

func captureNHCConfiguration(ctx context.Context) (string, map[string]interface{}) {
	nhc := upgradeNHC()
	Expect(APIClient.Get(ctx, client.ObjectKeyFromObject(nhc), nhc)).To(Succeed())
	spec, found, err := unstructured.NestedMap(nhc.Object, "spec")
	Expect(err).NotTo(HaveOccurred())
	Expect(found).To(BeTrue())
	return string(nhc.GetUID()), spec
}
