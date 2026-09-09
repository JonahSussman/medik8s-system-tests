package nhcutils

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/clients"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/nhc-operator/internal/nhcparams"
)

func RunOperatorSDK(ctx context.Context, binary string, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	command := exec.CommandContext(commandCtx, binary, args...)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		return output.String(), fmt.Errorf("%s %v: %w", binary, args, err)
	}
	return output.String(), nil
}

func InstallBundle(ctx context.Context, binary, namespace, bundle string) (string, error) {
	return RunOperatorSDK(ctx, binary, "run", "bundle", "-n", namespace, bundle)
}

func UpgradeBundle(ctx context.Context, binary, namespace, bundle string) (string, error) {
	return RunOperatorSDK(ctx, binary, "run", "bundle-upgrade", "-n", namespace, bundle)
}

// CleanupBundle intentionally leaves CRDs and the namespace OperatorGroup
// alone, since both can be shared by other operators.
func CleanupBundle(ctx context.Context, binary, namespace, packageName string) (string, error) {
	return RunOperatorSDK(ctx, binary, "cleanup", packageName, "-n", namespace,
		"--delete-all=false", "--delete-crds=false", "--delete-operator-groups=false")
}

// GetNHCControllerImage returns the manager image from a running controller.
func GetNHCControllerImage(apiClient *clients.Settings) (string, error) {
	return helpers.GetControllerImage(apiClient, "openshift-workload-availability",
		nhcparams.OperatorControllerPodLabelSelector, nhcparams.ManagerContainerName)
}

// CollectFailureEvidence is best-effort so the original assertion remains the
// reported failure when the local environment has no oc binary.
func CollectFailureEvidence(ctx context.Context, namespace string) string {
	output, err := RunCommand(ctx, "oc", "get", "subscriptions,clusterserviceversions,installplans,catalogsources,pods", "-n", namespace, "-o", "yaml")
	if err != nil {
		return fmt.Sprintf("OLM object collection failed: %v\n%s", err, output)
	}
	events, eventErr := RunCommand(ctx, "oc", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
	if eventErr != nil {
		return fmt.Sprintf("%s\nevent collection failed: %v\n%s", output, eventErr, events)
	}
	return output + "\nEvents:\n" + events
}

func RunCommand(ctx context.Context, binary string, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(commandCtx, binary, args...)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	return output.String(), command.Run()
}
