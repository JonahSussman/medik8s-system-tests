//nolint:wsl_v5 // Upgrade steps are grouped by lifecycle phase.
package tests

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/medik8s/system-tests/tests/internal/fbcsuite"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/mdr-operator/internal/mdrparams"
	. "github.com/onsi/ginkgo/v2"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	mdrFBCPackage          = "machine-deletion-remediation"
	mdrFBCSubscriptionName = "mdr-operator-upgrade-sub"
	mdrFBCNHCSubscription  = "mdr-upgrade-nhc-prerequisite"
)

type mdrUpgradeOperatorFBCTest struct {
	inputs             medik8sparams.FBCUpgradeInputs
	targetNode         string
	initialWorkerCount int
	workerNames        map[string]bool
	templateUID        types.UID
	templateSpec       map[string]interface{}
}

func (hooks *mdrUpgradeOperatorFBCTest) Setup(context.Context) error {
	return nil
}

func (hooks *mdrUpgradeOperatorFBCTest) FailureEvidence(context.Context) interface{} {
	return nil
}

func (hooks *mdrUpgradeOperatorFBCTest) BeforeUpgrade(ctx context.Context) error {
	if _, err := helpers.InstallGAOperatorSubscription(
		APIClient, mdrFBCNHCSubscription, medik8sparams.OperatorNs,
		medik8sparams.GAOperatorCatalog, medik8sparams.GACatalogNamespace,
		"node-healthcheck-operator", medik8sparams.GAChannel,
	); err != nil {
		return fmt.Errorf("install NHC prerequisite: %w", err)
	}
	if _, err := helpers.WaitForInstalledOperator(ctx, APIClient, helpers.OperatorOLMSpec{
		Package: "node-healthcheck-operator", SubscriptionName: mdrFBCNHCSubscription,
		Namespace: medik8sparams.OperatorNs, CSVNamePattern: "node-healthcheck-operator",
		Channel: medik8sparams.GAChannel,
	}, medik8sparams.OperatorUpgradeTimeout, mdrparams.DefaultPollInterval); err != nil {
		return err
	}

	platform, _, err := helpers.DetectPlatform(ctx, APIClient)
	if err != nil {
		return err
	}
	switch platform { //nolint:exhaustive // The default rejects every unsupported platform.
	case configv1.AWSPlatformType, configv1.AzurePlatformType,
		configv1.GCPPlatformType, configv1.VSpherePlatformType:
	default:
		return fmt.Errorf("MDR FBC upgrade remediation requires a Machine API cloud, got %s", platform)
	}

	hooks.initialWorkerCount, err = helpers.CountReadyWorkerNodes(ctx, APIClient)
	if err != nil {
		return err
	}
	if hooks.initialWorkerCount < 2 {
		return fmt.Errorf("MDR remediation requires two Ready workers, got %d", hooks.initialWorkerCount)
	}
	hooks.workerNames = map[string]bool{}
	nodes := &corev1.NodeList{}
	if err := APIClient.List(ctx, nodes,
		client.MatchingLabels{mdrparams.WorkerRoleLabel: ""}); err != nil {
		return err
	}
	for i := range nodes.Items {
		hooks.workerNames[nodes.Items[i].Name] = true
	}
	target, err := helpers.SelectWorkerNode(ctx, APIClient)
	if err != nil {
		return err
	}
	hooks.targetNode = target.Name

	template := buildMDRT(mdrparams.MDRTestTemplateName)
	if err := APIClient.Create(ctx, template); err != nil {
		return fmt.Errorf("create baseline MDR template: %w", err)
	}
	hooks.templateUID = template.GetUID()
	hooks.templateSpec, _, err = unstructured.NestedMap(template.Object, "spec")
	if err != nil {
		return err
	}

	if err := hooks.runRemediation(ctx, "baseline"); err != nil {
		return err
	}
	AddReportEntry("mdr-before-fbc-upgrade", map[string]interface{}{
		"templateUID": hooks.templateUID, "node": hooks.targetNode,
	})

	return nil
}

func (hooks *mdrUpgradeOperatorFBCTest) AfterUpgrade(ctx context.Context) error {
	if err := helpers.WaitForDeploymentImage(
		ctx, APIClient, medik8sparams.OperatorNs, mdrparams.OperatorDeploymentName,
		mdrparams.ManagerContainerName, hooks.inputs.CandidateImage,
		medik8sparams.OperatorUpgradeTimeout, mdrparams.DefaultPollInterval,
	); err != nil {
		return err
	}

	template := buildMDRT(mdrparams.MDRTestTemplateName)
	if err := APIClient.Get(ctx, client.ObjectKeyFromObject(template), template); err != nil {
		return fmt.Errorf("get MDR template after upgrade: %w", err)
	}
	spec, _, err := unstructured.NestedMap(template.Object, "spec")
	if err != nil || template.GetUID() != hooks.templateUID || !reflect.DeepEqual(spec, hooks.templateSpec) {
		return fmt.Errorf("MachineDeletionRemediationTemplate identity or spec changed across upgrade")
	}

	if err := hooks.runRemediation(ctx, "candidate"); err != nil {
		return err
	}
	AddReportEntry("mdr-after-fbc-upgrade", map[string]string{
		"version": hooks.inputs.CandidateVersion,
		"image":   hooks.inputs.CandidateImage,
		"node":    hooks.targetNode,
	})

	return nil
}

func (hooks *mdrUpgradeOperatorFBCTest) runRemediation(ctx context.Context, phase string) error {
	cleanupMDRCR(hooks.targetNode)
	cleanupNHCCR(mdrparams.NHCTestName)
	start := time.Now()
	nhc := buildNHCForMDR(mdrparams.NHCTestName, mdrparams.MDRTestTemplateName)
	if err := APIClient.Create(ctx, nhc); err != nil {
		return fmt.Errorf("create %s NHC trigger: %w", phase, err)
	}
	if err := stopKubeletForRemediation(ctx, hooks.targetNode); err != nil {
		return fmt.Errorf("stop kubelet for %s MDR remediation: %w", phase, err)
	}
	newNode, err := waitForMDRRemediationComplete(
		ctx, hooks.targetNode, hooks.initialWorkerCount, hooks.workerNames,
		start, mdrparams.RemediationCompleteTimeout)
	if err != nil {
		return fmt.Errorf("wait for %s MDR remediation: %w", phase, err)
	}
	if err := helpers.WaitForNodeReady(ctx, APIClient, newNode,
		mdrparams.DefaultPollInterval, mdrparams.NodeReadyTimeout, GinkgoWriter.Printf); err != nil {
		return err
	}
	delete(hooks.workerNames, hooks.targetNode)
	hooks.workerNames[newNode] = true
	hooks.targetNode = newNode
	cleanupNHCCR(mdrparams.NHCTestName)

	return nil
}

func (hooks *mdrUpgradeOperatorFBCTest) Cleanup(ctx context.Context) {
	cleanupNHCCR(mdrparams.NHCTestName)
	cleanupMDRT(mdrparams.MDRTestTemplateName)
	helpers.DeleteSubscription(APIClient, mdrFBCNHCSubscription, medik8sparams.OperatorNs, GinkgoWriter.Printf)
	if hooks.targetNode != "" {
		cleanupMDRCR(hooks.targetNode)
	}
	if hooks.initialWorkerCount > 0 {
		if err := waitForReadyWorkerCount(ctx, hooks.initialWorkerCount); err != nil {
			GinkgoWriter.Printf("WARNING: MDR worker count did not recover: %v\n", err)
		}
	}
}

func waitForReadyWorkerCount(ctx context.Context, expected int) error {
	return wait.PollUntilContextTimeout(
		ctx, mdrparams.DefaultPollInterval, mdrparams.NodeReadyTimeout, true,
		func(ctx context.Context) (bool, error) {
			count, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
			if err != nil {
				return false, nil
			}

			return count >= expected, nil
		})
}

func newMDRFBCTest(inputs medik8sparams.FBCUpgradeInputs) fbcsuite.UpgradeOperatorFBCTest {
	return &mdrUpgradeOperatorFBCTest{inputs: inputs}
}

var _ = fbcsuite.DefineFBCUpgradeSuite(fbcsuite.UpgradeOperatorFBCConfig{
	OperatorName:     "MDR",
	PackageName:      mdrFBCPackage,
	SubscriptionName: mdrFBCSubscriptionName,
	CSVNamePattern:   mdrparams.CSVNamePattern,
	DeploymentName:   mdrparams.OperatorDeploymentName,
	ContainerName:    mdrparams.ManagerContainerName,
	Labels: []string{
		labels.OperatorMDR, mdrparams.Label, labels.TierUpgradeOperator,
		labels.DisruptionDestructive, labels.PlatformAny, labels.ComponentOLM,
		labels.ComponentRemediation,
	},
	PolarionID:                "REPLACE_WITH_POLARION_ID",
	NewUpgradeOperatorFBCTest: newMDRFBCTest,
})
