package tests

import (
	"crypto/rand"

	"github.com/medik8s/system-tests/tests/internal/fbcsuite"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrutils"
)

func newSBRFBCTest(medik8sparams.FBCUpgradeInputs) fbcsuite.UpgradeOperatorFBCTest {
	owned := &sbrutils.OwnedRun{
		API: APIClient, Namespace: medik8sparams.OperatorNs, Token: rand.Text(),
	}

	return &sbrUpgradeOperatorFBCTest{
		owned: owned,
		token: owned.Token,
	}
}

var _ = fbcsuite.DefineFBCUpgradeSuite(fbcsuite.UpgradeOperatorFBCConfig{
	OperatorName:     "SBR",
	PackageName:      sbrparams.UpgradeSBRPackage,
	SubscriptionName: sbrFBCSubscriptionName,
	CSVNamePattern:   sbrparams.CSVNamePattern,
	DeploymentName:   sbrparams.OperatorDeploymentName,
	ContainerName:    sbrparams.ManagerContainerName,
	Labels: []string{
		labels.OperatorSBR, sbrparams.Label, labels.TierUpgradeOperator,
		labels.DisruptionNonDestructive, labels.PlatformAny, labels.ComponentOLM,
	},
	PolarionID:                "REPLACE_WITH_POLARION_ID",
	NewUpgradeOperatorFBCTest: newSBRFBCTest,
})
