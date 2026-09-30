//nolint:wsl_v5 // Upgrade steps are grouped by lifecycle phase.
package tests

import (
	"context"
	"fmt"

	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
)

const sbrFBCSubscriptionName = "sbr-operator-upgrade-sub"

// sbrUpgradeOperatorFBCTest exercises persistent configuration across an FBC upgrade.
type sbrUpgradeOperatorFBCTest struct {
	owned              *sbrutils.OwnedRun
	token              string
	configUID          string
	configSpec         map[string]interface{}
	baselineGeneration int64
}

func (hooks *sbrUpgradeOperatorFBCTest) Setup(ctx context.Context) error {
	if err := sbrutils.CheckClean(ctx, APIClient, hooks.owned.Namespace); err != nil {
		return err
	}

	return hooks.owned.CreateNamespace(ctx)
}

func (hooks *sbrUpgradeOperatorFBCTest) FailureEvidence(ctx context.Context) interface{} {
	return sbrutils.CollectFailureEvidence(ctx, hooks.owned.Namespace)
}

func (hooks *sbrUpgradeOperatorFBCTest) BeforeUpgrade(ctx context.Context) error {
	By("creating a safe, observable StorageBasedRemediationConfig")

	if err := waitForUpgradeAPI(ctx, upgradeSBRC()); err != nil {
		return fmt.Errorf("wait for SBR API: %w", err)
	}

	storageClass := discoverRWXStorageClass()
	sbrc := buildSBRC(sbrparams.SBRUpgradeConfigTestName, sbrutils.SafeSpec(hooks.token, storageClass))
	if err := hooks.owned.Create(ctx, sbrc); err != nil {
		return fmt.Errorf("create StorageBasedRemediationConfig: %w", err)
	}

	hooks.baselineGeneration = waitForUpgradeAgentDaemonSetExists(ctx)
	hooks.configUID, hooks.configSpec = captureSBRCConfiguration(ctx)
	AddReportEntry("sbr-config-before-operator-upgrade", map[string]interface{}{
		"uid": hooks.configUID, "spec": hooks.configSpec,
	})
	GinkgoWriter.Printf("SBR config before operator upgrade: uid=%s spec=%v\n",
		hooks.configUID, hooks.configSpec)

	return nil
}

func (hooks *sbrUpgradeOperatorFBCTest) AfterUpgrade(ctx context.Context) error {
	By("verifying the same configuration identity, specification, and reconciliation")
	uid, spec := captureSBRCConfiguration(ctx)
	if uid != hooks.configUID {
		return fmt.Errorf("StorageBasedRemediationConfig UID changed from %s to %s", hooks.configUID, uid)
	}
	Expect(spec).To(Equal(hooks.configSpec),
		"upgrade must preserve the StorageBasedRemediationConfig specification")

	By("requiring a fresh candidate-controller response to a probe patch")
	probeGeneration := patchSBRCMaxConsecutiveFailures(
		ctx, types.UID(hooks.configUID), hooks.baselineGeneration,
		int64(sbrparams.SBRCMaxConsecutiveFailuresMin+1))

	By("restoring the original configuration and requiring another controller response")
	patchSBRCMaxConsecutiveFailures(
		ctx, types.UID(hooks.configUID), probeGeneration,
		int64(sbrparams.SBRCMaxConsecutiveFailuresMin))
	uid, spec = captureSBRCConfiguration(ctx)
	Expect(uid).To(Equal(hooks.configUID))
	Expect(spec).To(Equal(hooks.configSpec))
	AddReportEntry("sbr-config-after-operator-upgrade", map[string]interface{}{
		"uid": uid, "spec": spec,
	})
	GinkgoWriter.Printf("SBR config after operator upgrade: uid=%s spec=%v\n", uid, spec)

	return nil
}

func (hooks *sbrUpgradeOperatorFBCTest) Cleanup(ctx context.Context) {
	if err := hooks.owned.Cleanup(ctx); err != nil {
		AddReportEntry("sbr-upgrade-operator-cleanup-failure", err.Error())
		AddReportEntry("sbr-upgrade-operator-cleanup-evidence", hooks.FailureEvidence(ctx))
	}
}
