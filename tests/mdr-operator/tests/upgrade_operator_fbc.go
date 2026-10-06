package tests

import (
	"crypto/rand"
	"strings"

	"github.com/medik8s/system-tests/tests/internal/fbcsuite"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/mdr-operator/internal/mdrparams"
)

func newMDRFBCTest(inputs medik8sparams.FBCUpgradeInputs) fbcsuite.UpgradeOperatorFBCTest {
	return &mdrUpgradeOperatorFBCTest{
		owned: &helpers.FBCNamespace{
			API: APIClient, Name: medik8sparams.OperatorNs, Token: strings.ToLower(rand.Text()),
		},
	}
}

var _ = fbcsuite.DefineFBCUpgradeSuite(fbcsuite.UpgradeOperatorFBCConfig{
	OperatorName:     "MDR",
	PackageName:      "machine-deletion-remediation",
	SubscriptionName: "mdr-operator-upgrade-sub",
	CSVNamePattern:   mdrparams.CSVNamePattern,
	DeploymentName:   mdrparams.OperatorDeploymentName,
	ContainerName:    mdrparams.ManagerContainerName,
	Labels: []string{
		labels.OperatorMDR, mdrparams.Label, labels.TierUpgradeOperator,
		labels.DisruptionNonDestructive, labels.PlatformAny, labels.ComponentOLM,
	},
	PolarionID:                "REPLACE_WITH_POLARION_ID",
	NewUpgradeOperatorFBCTest: newMDRFBCTest,
})
