package farutils

import (
	"fmt"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/clients"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	olmV1alpha1 "github.com/rh-ecosystem-edge/eco-goinfra/pkg/schemes/olm/operators/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
)

// InstallGAOperator creates a Subscription for the FAR operator from the built-in
// redhat-operators catalog on the current OCP cluster.
func InstallGAOperator(apiClient *clients.Settings) (*olm.SubscriptionBuilder, error) {
	sub := olm.NewSubscriptionBuilder(
		apiClient,
		farparams.UpgradeSubName,
		medik8sparams.OperatorNs,
		farparams.GAOperatorCatalog,
		farparams.GACatalogNamespace,
		farparams.OperatorPackage,
	)

	sub.WithChannel(farparams.GAChannel).
		WithInstallPlanApproval(olmV1alpha1.ApprovalAutomatic)

	sub, err := sub.Create()
	if err != nil {
		return nil, fmt.Errorf("failed to create GA Subscription: %w", err)
	}

	return sub, nil
}

// FindSucceededCSV returns the first CSV matching the given name pattern that is
// in the Succeeded phase. Returns an error if no matching CSV is found. Callers
// should wrap this in Eventually for polling behavior.
func FindSucceededCSV(
	apiClient *clients.Settings, namePattern string,
) (*olm.ClusterServiceVersionBuilder, error) {
	csvs, err := olm.ListClusterServiceVersionWithNamePattern(
		apiClient, namePattern, medik8sparams.OperatorNs)
	if err != nil {
		return nil, fmt.Errorf("failed to list CSVs matching %q: %w", namePattern, err)
	}

	for _, csv := range csvs {
		phase, phaseErr := csv.GetPhase()
		if phaseErr == nil && phase == olmV1alpha1.CSVPhaseSucceeded {
			return csv, nil
		}
	}

	return nil, fmt.Errorf("no CSV matching %q in Succeeded phase", namePattern)
}

// SwitchSubscriptionCatalog updates an existing Subscription to point to the
// given CatalogSource name and target channel.
func SwitchSubscriptionCatalog(
	apiClient *clients.Settings, catalogName string,
) (*olm.SubscriptionBuilder, error) {
	sub, err := olm.PullSubscription(
		apiClient, farparams.UpgradeSubName, medik8sparams.OperatorNs)
	if err != nil {
		return nil, fmt.Errorf("failed to pull Subscription: %w", err)
	}

	sub.Definition.Spec.CatalogSource = catalogName
	sub.Definition.Spec.Channel = farparams.TargetChannel

	sub, err = sub.Update()
	if err != nil {
		return nil, fmt.Errorf("failed to update Subscription to target catalog: %w", err)
	}

	return sub, nil
}

// GetFARControllerImage returns the manager container image of the first running
// FAR controller pod, matching by farparams.ManagerContainerName.
func GetFARControllerImage(apiClient *clients.Settings) (string, error) {
	listOptions := metav1.ListOptions{
		LabelSelector: farparams.OperatorControllerPodLabelSelector,
	}

	farPods, err := pod.List(apiClient, medik8sparams.OperatorNs, listOptions)
	if err != nil {
		return "", fmt.Errorf("failed to list FAR controller pods: %w", err)
	}

	runningPods := helpers.FilterRunningPods(farPods)
	if len(runningPods) == 0 {
		return "", fmt.Errorf("no running FAR controller pods found")
	}

	for _, container := range runningPods[0].Object.Spec.Containers {
		if container.Name == farparams.ManagerContainerName {
			return container.Image, nil
		}
	}

	return "", fmt.Errorf("container %s not found in FAR controller pod",
		farparams.ManagerContainerName)
}

// CleanupUpgradeResources removes the Subscription created during the upgrade
// test. The CatalogSource is owned by the CI step and not deleted here.
func CleanupUpgradeResources(apiClient *clients.Settings, logf func(string, ...interface{})) {
	sub, err := olm.PullSubscription(
		apiClient, farparams.UpgradeSubName, medik8sparams.OperatorNs)
	if err != nil {
		logf("WARNING: failed to pull upgrade Subscription for cleanup: %v\n", err)

		return
	}

	if delErr := sub.Delete(); delErr != nil {
		logf("WARNING: failed to delete upgrade Subscription: %v\n", delErr)
	}
}
