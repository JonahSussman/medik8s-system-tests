//nolint:wsl_v5 // Upgrade steps are grouped by lifecycle phase.
package tests

import (
	"context"
	"fmt"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
	"github.com/medik8s/system-tests/tests/far-operator/internal/farutils"
	"github.com/medik8s/system-tests/tests/internal/fbcsuite"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	. "github.com/onsi/ginkgo/v2"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const farFBCPackage = "fence-agents-remediation"

type farUpgradeOperatorFBCTest struct {
	platform       configv1.PlatformType
	region         string
	credentials    *corev1.Secret
	currentFARName string
}

func (hooks *farUpgradeOperatorFBCTest) Setup(context.Context) error {
	return nil
}

func (hooks *farUpgradeOperatorFBCTest) FailureEvidence(context.Context) interface{} {
	return nil
}

func (hooks *farUpgradeOperatorFBCTest) prepare(ctx context.Context) error {
	if hooks.platform == "" {
		var err error
		hooks.platform, hooks.region, err = helpers.DetectPlatform(ctx, APIClient)
		if err != nil {
			return err
		}
		if hooks.platform != configv1.AWSPlatformType {
			return fmt.Errorf("FAR FBC upgrade remediation requires AWS, got %s", hooks.platform)
		}

		workerCount, countErr := helpers.CountReadyWorkerNodes(ctx, APIClient)
		if countErr != nil {
			return countErr
		}
		if workerCount < 3 {
			return fmt.Errorf("FAR FBC upgrade remediation requires three Ready workers, got %d", workerCount)
		}

		accessKey, secretKey, credentialErr := farutils.GetAWSCredentials(
			ctx, APIClient, medik8sparams.OperatorNs)
		if credentialErr != nil {
			return credentialErr
		}
		hooks.credentials = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: farparams.SharedCredentialsSecretName, Namespace: medik8sparams.OperatorNs,
			},
			StringData: map[string]string{"--access-key": accessKey, "--secret-key": secretKey},
		}
		if err := APIClient.Create(ctx, hooks.credentials); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create FAR credentials Secret: %w", err)
		}
	}

	fenceAgent, sharedParams, nodeParams, leaderNode, err :=
		upgradeProvisionRemediationResources(ctx, hooks.platform, hooks.region)
	if err != nil {
		return err
	}
	hooks.currentFARName, err = upgradeRunRemediationCycle(
		ctx, fenceAgent, sharedParams, nodeParams, leaderNode, "FBC-operator-upgrade")
	if err != nil {
		return err
	}
	cleanupPostRemediation(ctx, &hooks.currentFARName, "FBC-operator-upgrade")

	return nil
}

func (hooks *farUpgradeOperatorFBCTest) BeforeUpgrade(ctx context.Context) error {
	if err := hooks.prepare(ctx); err != nil {
		return fmt.Errorf("baseline FAR remediation: %w", err)
	}
	AddReportEntry("far-before-fbc-upgrade", hooks.platform)

	return nil
}

func (hooks *farUpgradeOperatorFBCTest) AfterUpgrade(ctx context.Context) error {
	if err := hooks.prepare(ctx); err != nil {
		return fmt.Errorf("candidate FAR remediation: %w", err)
	}

	return nil
}

func (hooks *farUpgradeOperatorFBCTest) Cleanup(ctx context.Context) {
	if hooks.currentFARName != "" {
		nodeName := hooks.currentFARName
		farutils.CleanupFARRemediation(ctx, APIClient, farGVK, hooks.currentFARName,
			medik8sparams.OperatorNs, GinkgoWriter.Printf)
		hooks.currentFARName = ""
		if err := farutils.WaitForNodeReady(ctx, APIClient, nodeName,
			farparams.NodeReadyTimeout, GinkgoWriter.Printf); err != nil {
			GinkgoWriter.Printf("WARNING: FAR upgrade target did not recover: %v\n", err)
		}
	}
	if hooks.credentials != nil {
		if err := APIClient.Delete(ctx, hooks.credentials); err != nil && !apierrors.IsNotFound(err) {
			GinkgoWriter.Printf("WARNING: FAR credentials Secret cleanup failed: %v\n", err)
		}
	}
}

func newFARFBCTest(medik8sparams.FBCUpgradeInputs) fbcsuite.UpgradeOperatorFBCTest {
	return &farUpgradeOperatorFBCTest{}
}

var _ = fbcsuite.DefineFBCUpgradeSuite(fbcsuite.UpgradeOperatorFBCConfig{
	OperatorName:     "FAR",
	PackageName:      farFBCPackage,
	SubscriptionName: farparams.UpgradeSubName,
	CSVNamePattern:   farFBCPackage,
	DeploymentName:   farparams.OperatorDeploymentName,
	ContainerName:    farparams.ManagerContainerName,
	Labels: []string{
		labels.OperatorFAR, farparams.Label, labels.TierUpgradeOperator,
		labels.DisruptionDestructive, labels.PlatformAWS, labels.ComponentOLM,
		labels.ComponentRemediation,
	},
	PolarionID:                "REPLACE_WITH_POLARION_ID",
	NewUpgradeOperatorFBCTest: newFARFBCTest,
})
