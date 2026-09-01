package tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrparams"
)

// sbrUpgradeFailureDump is used as an upgrade spec's JustAfterEach: dumps SBR
// controller state when the spec failed, to aid debugging.
func sbrUpgradeFailureDump(ctx context.Context) {
	if !CurrentSpecReport().Failed() {
		return
	}

	GinkgoWriter.Println("Upgrade test failed - collecting SBR controller state")
	helpers.LogControllerState(ctx, APIClient,
		medik8sparams.OperatorNs,
		map[string]string{
			"app.kubernetes.io/name": sbrparams.OperatorControllerPodLabel,
			"control-plane":          "controller-manager",
		},
		GinkgoWriter.Printf)
}

// runSBRCFunctionCheck creates (or reuses) the upgrade-test SBRC and waits for
// its agent DaemonSet to reach Ready, then tears it down -- proving the
// controller, agent image, and DaemonSet-management chain all function at
// this point in the upgrade sequence.
func runSBRCFunctionCheck(phase string) {
	storageClass := discoverRWXStorageClass()

	By(fmt.Sprintf("[%s] Creating SBRC %s (storageClass=%s)", phase, sbrparams.UpgradeSBRCName, storageClass))

	sbrc := buildSBRC(sbrparams.UpgradeSBRCName, map[string]interface{}{
		"sharedStorageClass": storageClass,
	})
	Expect(APIClient.Create(context.TODO(), sbrc)).To(Succeed(),
		"[%s] Failed to create SBRC %s", phase, sbrparams.UpgradeSBRCName)

	By(fmt.Sprintf("[%s] Waiting for SBRC %s agent DaemonSet to be Ready", phase, sbrparams.UpgradeSBRCName))

	waitForSBRCReady(sbrparams.UpgradeSBRCName)

	GinkgoWriter.Printf("[%s] SBRC %s agent DaemonSet Ready\n", phase, sbrparams.UpgradeSBRCName)

	cleanupUpgradeSBRC()
}

// cleanupUpgradeSBRC deletes the upgrade-test SBRC and waits for its agent
// DaemonSet to be garbage-collected, so each phase starts from a clean slate.
func cleanupUpgradeSBRC() {
	sbrc := buildSBRC(sbrparams.UpgradeSBRCName, map[string]interface{}{})

	if delErr := APIClient.Delete(context.TODO(), sbrc); delErr != nil && !k8serrors.IsNotFound(delErr) {
		GinkgoWriter.Printf("WARNING: failed to delete SBRC %s: %v\n", sbrparams.UpgradeSBRCName, delErr)
	}

	dsName := sbrparams.SBRAgentDaemonSetPrefix + sbrparams.UpgradeSBRCName

	Eventually(func() bool {
		_, getErr := APIClient.DaemonSets(medik8sparams.OperatorNs).Get(
			context.TODO(), dsName, metav1.GetOptions{})

		return k8serrors.IsNotFound(getErr)
	}, sbrparams.SBRCDaemonSetGCTimeout, sbrparams.DefaultPollInterval).Should(BeTrue(),
		"agent DaemonSet %s was not garbage-collected after SBRC deletion", dsName)
}
