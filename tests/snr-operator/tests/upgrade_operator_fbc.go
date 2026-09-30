//nolint:wsl_v5 // Upgrade steps are grouped by lifecycle phase.
package tests

import (
	"context"
	"fmt"
	"reflect"

	"github.com/medik8s/system-tests/tests/internal/fbcsuite"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/snr-operator/internal/snrparams"
	. "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	snrFBCPackage          = "self-node-remediation"
	snrFBCSubscriptionName = "snr-operator-upgrade-sub"
	snrFBCNHCSubscription  = "snr-upgrade-nhc-prerequisite"
)

type snrUpgradeOperatorFBCTest struct {
	inputs     medik8sparams.FBCUpgradeInputs
	targetNode string
	configUID  types.UID
	configSpec map[string]interface{}
}

func (hooks *snrUpgradeOperatorFBCTest) Setup(context.Context) error {
	return nil
}

func (hooks *snrUpgradeOperatorFBCTest) FailureEvidence(context.Context) interface{} {
	return nil
}

func (hooks *snrUpgradeOperatorFBCTest) BeforeUpgrade(ctx context.Context) error {
	if _, err := helpers.InstallGAOperatorSubscription(
		APIClient, snrFBCNHCSubscription, medik8sparams.OperatorNs,
		medik8sparams.GAOperatorCatalog, medik8sparams.GACatalogNamespace,
		"node-healthcheck-operator", medik8sparams.GAChannel,
	); err != nil {
		return fmt.Errorf("install NHC prerequisite: %w", err)
	}

	if _, err := helpers.WaitForInstalledOperator(ctx, APIClient, helpers.OperatorOLMSpec{
		Package: "node-healthcheck-operator", SubscriptionName: snrFBCNHCSubscription,
		Namespace: medik8sparams.OperatorNs, CSVNamePattern: "node-healthcheck-operator",
		Channel: medik8sparams.GAChannel,
	}, medik8sparams.OperatorUpgradeTimeout, snrparams.DefaultPollInterval); err != nil {
		return err
	}

	config := &unstructured.Unstructured{}
	config.SetGroupVersionKind(snrcGVK)
	if err := APIClient.Get(ctx, client.ObjectKey{
		Name: snrparams.SNRConfigName, Namespace: medik8sparams.OperatorNs,
	}, config); err != nil {
		return fmt.Errorf("get baseline SelfNodeRemediationConfig: %w", err)
	}
	hooks.configUID = config.GetUID()
	var found bool
	var err error
	hooks.configSpec, found, err = unstructured.NestedMap(config.Object, "spec")
	if err != nil || !found {
		return fmt.Errorf("read baseline SelfNodeRemediationConfig spec: found=%t: %w", found, err)
	}

	if err := hooks.runRemediation(ctx, "baseline"); err != nil {
		return err
	}
	AddReportEntry("snr-before-fbc-upgrade", map[string]interface{}{
		"uid": hooks.configUID, "spec": hooks.configSpec, "node": hooks.targetNode,
	})

	return nil
}

func (hooks *snrUpgradeOperatorFBCTest) AfterUpgrade(ctx context.Context) error {
	if err := helpers.WaitForDeploymentImage(
		ctx, APIClient, medik8sparams.OperatorNs, snrparams.OperatorDeploymentName,
		snrparams.ManagerContainerName, hooks.inputs.CandidateImage,
		medik8sparams.OperatorUpgradeTimeout, snrparams.DefaultPollInterval,
	); err != nil {
		return err
	}

	if err := waitForSNRPodsImage(ctx, hooks.inputs.CandidateImage); err != nil {
		return err
	}

	config := &unstructured.Unstructured{}
	config.SetGroupVersionKind(snrcGVK)
	if err := APIClient.Get(ctx, client.ObjectKey{
		Name: snrparams.SNRConfigName, Namespace: medik8sparams.OperatorNs,
	}, config); err != nil {
		return fmt.Errorf("get SelfNodeRemediationConfig after upgrade: %w", err)
	}
	spec, found, err := unstructured.NestedMap(config.Object, "spec")
	if err != nil || !found || config.GetUID() != hooks.configUID || !reflect.DeepEqual(spec, hooks.configSpec) {
		return fmt.Errorf("SelfNodeRemediationConfig identity or spec changed across upgrade")
	}

	if err := hooks.runRemediation(ctx, "candidate"); err != nil {
		return err
	}
	AddReportEntry("snr-after-fbc-upgrade", map[string]interface{}{
		"version": hooks.inputs.CandidateVersion,
		"image":   hooks.inputs.CandidateImage,
		"node":    hooks.targetNode,
	})

	return nil
}

func (hooks *snrUpgradeOperatorFBCTest) runRemediation(ctx context.Context, phase string) error {
	workerCount, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
	if err != nil || workerCount < 2 {
		return fmt.Errorf("%s remediation requires at least two Ready workers: count=%d: %w",
			phase, workerCount, err)
	}

	target, err := helpers.SelectWorkerNode(ctx, APIClient)
	if err != nil {
		return fmt.Errorf("select %s remediation worker: %w", phase, err)
	}
	hooks.targetNode = target.Name
	if err := helpers.RemoveKubeletStopGuard(ctx, hooks.targetNode, snrparams.OcDebugTimeout); err != nil {
		return fmt.Errorf("remove kubelet stop guard: %w", err)
	}

	oldBootID, err := helpers.GetNodeBootIDFromAPI(ctx, APIClient, hooks.targetNode)
	if err != nil {
		return err
	}
	node := &corev1.Node{}
	if err := APIClient.Get(ctx, client.ObjectKey{Name: hooks.targetNode}, node); err != nil {
		return err
	}
	creationTime := node.CreationTimestamp

	cleanupSNRCR(hooks.targetNode)
	cleanupNHCCR(snrparams.NHCTestName)
	nhc := buildNHCForWorkers(snrparams.NHCTestName, snrparams.SNRTemplateName)
	if err := APIClient.Create(ctx, nhc); err != nil {
		return fmt.Errorf("create %s NHC trigger: %w", phase, err)
	}
	if err := stopKubeletForRemediation(ctx, hooks.targetNode); err != nil {
		return fmt.Errorf("stop kubelet for %s remediation: %w", phase, err)
	}
	if err := waitForRemediationComplete(ctx, APIClient, hooks.targetNode, oldBootID); err != nil {
		return fmt.Errorf("wait for %s remediation: %w", phase, err)
	}
	if err := helpers.WaitForNodeReady(ctx, APIClient, hooks.targetNode,
		snrparams.DefaultPollInterval, snrparams.NodeReadyTimeout, GinkgoWriter.Printf); err != nil {
		return err
	}
	updated := &corev1.Node{}
	if err := APIClient.Get(ctx, client.ObjectKey{Name: hooks.targetNode}, updated); err != nil {
		return err
	}
	if !updated.CreationTimestamp.Equal(&creationTime) {
		return fmt.Errorf("node %s was replaced instead of rebooted", hooks.targetNode)
	}
	cleanupNHCCR(snrparams.NHCTestName)
	bestEffortRemoveKubeletStopGuard(ctx, hooks.targetNode)
	hooks.targetNode = ""

	return nil
}

func (hooks *snrUpgradeOperatorFBCTest) Cleanup(ctx context.Context) {
	cleanupNHCCR(snrparams.NHCTestName)
	helpers.DeleteSubscription(APIClient, snrFBCNHCSubscription, medik8sparams.OperatorNs, GinkgoWriter.Printf)
	if hooks.targetNode != "" {
		cleanupSNRCR(hooks.targetNode)
		if err := helpers.WaitForNodeReady(ctx, APIClient, hooks.targetNode,
			snrparams.DefaultPollInterval, snrparams.NodeReadyTimeout, GinkgoWriter.Printf); err != nil {
			GinkgoWriter.Printf("WARNING: SNR upgrade target did not recover: %v\n", err)
		}
		bestEffortRemoveKubeletStopGuard(ctx, hooks.targetNode)
	}
}

func waitForSNRPodsImage(ctx context.Context, expectedImage string) error {
	return wait.PollUntilContextTimeout(
		ctx, snrparams.DefaultPollInterval, medik8sparams.OperatorUpgradeTimeout, true,
		func(ctx context.Context) (bool, error) {
			pods := &corev1.PodList{}
			if err := APIClient.List(ctx, pods, client.InNamespace(medik8sparams.OperatorNs),
				client.MatchingLabels{"app.kubernetes.io/name": "self-node-remediation",
					"app.kubernetes.io/component": "agent"}); err != nil {
				return false, nil
			}
			if len(pods.Items) == 0 {
				return false, nil
			}
			for _, pod := range pods.Items {
				for _, container := range pod.Spec.Containers {
					if container.Image != expectedImage {
						return false, nil
					}
				}
			}

			return true, nil
		})
}

func newSNRFBCTest(inputs medik8sparams.FBCUpgradeInputs) fbcsuite.UpgradeOperatorFBCTest {
	return &snrUpgradeOperatorFBCTest{inputs: inputs}
}

var _ = fbcsuite.DefineFBCUpgradeSuite(fbcsuite.UpgradeOperatorFBCConfig{
	OperatorName:     "SNR",
	PackageName:      snrFBCPackage,
	SubscriptionName: snrFBCSubscriptionName,
	CSVNamePattern:   snrparams.CSVNamePattern,
	DeploymentName:   snrparams.OperatorDeploymentName,
	ContainerName:    snrparams.ManagerContainerName,
	Labels: []string{
		labels.OperatorSNR, snrparams.Label, labels.TierUpgradeOperator,
		labels.DisruptionDestructive, labels.PlatformAny, labels.ComponentOLM,
		labels.ComponentRemediation,
	},
	PolarionID:                "REPLACE_WITH_POLARION_ID",
	NewUpgradeOperatorFBCTest: newSNRFBCTest,
})
