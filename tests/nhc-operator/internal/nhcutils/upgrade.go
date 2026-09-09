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

// CleanupBundle intentionally leaves CRDs alone, since they can be shared.
func CleanupBundle(ctx context.Context, binary, namespace, packageName string) (string, error) {
	return RunOperatorSDK(ctx, binary, "cleanup", packageName, "-n", namespace)
}

// GetNHCControllerImage returns the manager image from a running controller.
func GetNHCControllerImage(apiClient *clients.Settings) (string, error) {
	return helpers.GetControllerImage(apiClient, "openshift-workload-availability",
		nhcparams.OperatorControllerPodLabelSelector, nhcparams.ManagerContainerName)
}
