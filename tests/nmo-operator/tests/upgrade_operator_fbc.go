//nolint:wsl_v5 // Upgrade steps are grouped by lifecycle phase.
package tests

import (
	"context"
	"fmt"
	"reflect"

	nmov1beta1 "github.com/medik8s/node-maintenance-operator/api/v1beta1"
	"github.com/medik8s/system-tests/tests/internal/fbcsuite"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/nmo-operator/internal/nmoparams"
	. "github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	nmoFBCPackage          = "node-maintenance-operator"
	nmoFBCSubscriptionName = "nmo-operator-upgrade-sub"
	nmoUpgradeResourceName = "nmo-operator-upgrade"
)

type nmoUpgradeOperatorFBCTest struct {
	inputs     medik8sparams.FBCUpgradeInputs
	targetNode string
	uid        types.UID
	spec       nmov1beta1.NodeMaintenanceSpec
}

func (hooks *nmoUpgradeOperatorFBCTest) Setup(context.Context) error {
	return nil
}

func (hooks *nmoUpgradeOperatorFBCTest) FailureEvidence(context.Context) interface{} {
	return nil
}

func (hooks *nmoUpgradeOperatorFBCTest) BeforeUpgrade(ctx context.Context) error {
	if err := APIClient.AttachScheme(nmov1beta1.AddToScheme); err != nil {
		return fmt.Errorf("attach NMO API scheme: %w", err)
	}

	hooks.targetNode = selectSchedulableWorker(ctx)
	resource := &nmov1beta1.NodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{Name: nmoUpgradeResourceName},
		Spec: nmov1beta1.NodeMaintenanceSpec{
			NodeName: hooks.targetNode,
			Reason:   "FBC operator upgrade persistence check",
		},
	}
	if err := APIClient.Create(ctx, resource); err != nil {
		return fmt.Errorf("create baseline NodeMaintenance: %w", err)
	}

	waitForMaintenanceSucceeded(ctx, nmoUpgradeResourceName)
	assertNodeCordonAndTaint(hooks.targetNode, true, nmoparams.MaintenanceTimeout)
	assertDrainCompleted(ctx, nmoUpgradeResourceName)
	assertMaintenanceLease(ctx, hooks.targetNode, true)

	current := &nmov1beta1.NodeMaintenance{}
	if err := APIClient.Get(ctx, client.ObjectKey{Name: nmoUpgradeResourceName}, current); err != nil {
		return fmt.Errorf("get baseline NodeMaintenance: %w", err)
	}
	hooks.uid = current.UID
	hooks.spec = current.Spec
	AddReportEntry("nmo-before-fbc-upgrade", map[string]interface{}{
		"uid": hooks.uid, "spec": hooks.spec, "node": hooks.targetNode,
	})

	return nil
}

func (hooks *nmoUpgradeOperatorFBCTest) AfterUpgrade(ctx context.Context) error {
	if err := helpers.WaitForDeploymentImage(
		ctx, APIClient, medik8sparams.OperatorNs, nmoparams.OperatorDeploymentName,
		nmoparams.ManagerContainerName, hooks.inputs.CandidateImage,
		medik8sparams.OperatorUpgradeTimeout, nmoparams.DefaultPollInterval,
	); err != nil {
		return err
	}

	current := &nmov1beta1.NodeMaintenance{}
	if err := APIClient.Get(ctx, client.ObjectKey{Name: nmoUpgradeResourceName}, current); err != nil {
		return fmt.Errorf("get NodeMaintenance after upgrade: %w", err)
	}
	if current.UID != hooks.uid || !reflect.DeepEqual(current.Spec, hooks.spec) {
		return fmt.Errorf("NodeMaintenance identity or spec changed across upgrade")
	}
	waitForMaintenanceSucceeded(ctx, nmoUpgradeResourceName)
	assertNodeCordonAndTaint(hooks.targetNode, true, nmoparams.MaintenanceTimeout)
	assertDrainCompleted(ctx, nmoUpgradeResourceName)
	assertMaintenanceLease(ctx, hooks.targetNode, true)

	deleteAndWaitForNMCR(ctx, nmoUpgradeResourceName, nmoparams.UncordonTimeout)
	assertNodeCordonAndTaint(hooks.targetNode, false, nmoparams.UncordonTimeout)
	assertMaintenanceLease(ctx, hooks.targetNode, false)

	probe := &nmov1beta1.NodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{Name: nmoUpgradeResourceName},
		Spec: nmov1beta1.NodeMaintenanceSpec{
			NodeName: hooks.targetNode,
			Reason:   "candidate controller reconciliation check",
		},
	}
	if err := APIClient.Create(ctx, probe); err != nil {
		return fmt.Errorf("create candidate NodeMaintenance probe: %w", err)
	}
	waitForMaintenanceSucceeded(ctx, nmoUpgradeResourceName)
	assertNodeCordonAndTaint(hooks.targetNode, true, nmoparams.MaintenanceTimeout)
	deleteAndWaitForNMCR(ctx, nmoUpgradeResourceName, nmoparams.UncordonTimeout)
	assertNodeCordonAndTaint(hooks.targetNode, false, nmoparams.UncordonTimeout)
	AddReportEntry("nmo-after-fbc-upgrade", map[string]interface{}{
		"version": hooks.inputs.CandidateVersion,
		"image":   hooks.inputs.CandidateImage,
		"node":    hooks.targetNode,
	})

	return nil
}

func (hooks *nmoUpgradeOperatorFBCTest) Cleanup(ctx context.Context) {
	deleteAndWaitForNMCR(ctx, nmoUpgradeResourceName, nmoparams.UncordonTimeout)
	if hooks.targetNode != "" {
		waitForNodeReadyAndUncordoned(ctx, hooks.targetNode, nmoparams.RebootTimeout)
	}
}

func newNMOFBCTest(inputs medik8sparams.FBCUpgradeInputs) fbcsuite.UpgradeOperatorFBCTest {
	return &nmoUpgradeOperatorFBCTest{inputs: inputs}
}

var _ = fbcsuite.DefineFBCUpgradeSuite(fbcsuite.UpgradeOperatorFBCConfig{
	OperatorName:     "NMO",
	PackageName:      nmoFBCPackage,
	SubscriptionName: nmoFBCSubscriptionName,
	CSVNamePattern:   nmoparams.CSVNamePattern,
	DeploymentName:   nmoparams.OperatorDeploymentName,
	ContainerName:    nmoparams.ManagerContainerName,
	Labels: []string{
		labels.OperatorNMO, nmoparams.Label, labels.TierUpgradeOperator,
		labels.DisruptionDestructive, labels.PlatformAny, labels.ComponentOLM,
		labels.ComponentController,
	},
	PolarionID:                "REPLACE_WITH_POLARION_ID",
	NewUpgradeOperatorFBCTest: newNMOFBCTest,
})
