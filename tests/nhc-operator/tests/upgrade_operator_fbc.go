package tests

import (
	"context"
	"crypto/rand"

	"github.com/medik8s/system-tests/tests/internal/fbcsuite"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/nhc-operator/internal/nhcparams"
	"github.com/medik8s/system-tests/tests/nhc-operator/internal/nhcutils"
)

func newNHCFBCTest(medik8sparams.FBCUpgradeInputs) fbcsuite.UpgradeOperatorFBCTest {
	owned := &nhcutils.OwnedRun{
		API: APIClient, Namespace: medik8sparams.OperatorNs, Token: rand.Text(),
	}

	return &nhcUpgradeOperatorFBCTest{
		owned:     owned,
		namespace: medik8sparams.OperatorNs,
		token:     owned.Token,
		prepareSNR: func(context.Context) error {
			_, err := nhcutils.InstallGASNR(APIClient)

			return err
		},
	}
}

var _ = fbcsuite.DefineFBCUpgradeSuite(fbcsuite.UpgradeOperatorFBCConfig{
	OperatorName:     "NHC",
	PackageName:      nhcparams.UpgradeNHCPackage,
	SubscriptionName: nhcparams.ClusterUpgradeSubName,
	CSVNamePattern:   nhcparams.CSVNamePattern,
	DeploymentName:   nhcparams.OperatorDeploymentName,
	ContainerName:    nhcparams.ManagerContainerName,
	Labels: []string{
		labels.OperatorNHC, nhcparams.Label, labels.TierUpgradeOperator,
		labels.DisruptionDestructive, labels.PlatformAny, labels.ComponentOLM,
		labels.ComponentRemediation,
	},
	PolarionID:                "REPLACE_WITH_POLARION_ID",
	NewUpgradeOperatorFBCTest: newNHCFBCTest,
})
