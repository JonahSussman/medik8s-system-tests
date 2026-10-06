package tests

import (
	"crypto/rand"
	"strings"

	"github.com/medik8s/system-tests/tests/internal/fbcsuite"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/nmo-operator/internal/nmoparams"
)

func newNMOFBCTest(inputs medik8sparams.FBCUpgradeInputs) fbcsuite.UpgradeOperatorFBCTest {
	return &nmoUpgradeOperatorFBCTest{
		owned: &helpers.FBCNamespace{
			API: APIClient, Name: medik8sparams.OperatorNs, Token: strings.ToLower(rand.Text()),
		},
	}
}

var _ = fbcsuite.DefineFBCUpgradeSuite(fbcsuite.UpgradeOperatorFBCConfig{
	OperatorName:     "NMO",
	PackageName:      "node-maintenance-operator",
	SubscriptionName: "nmo-operator-upgrade-sub",
	CSVNamePattern:   nmoparams.CSVNamePattern,
	DeploymentName:   nmoparams.OperatorDeploymentName,
	ContainerName:    nmoparams.ManagerContainerName,
	Labels: []string{
		labels.OperatorNMO, nmoparams.Label, labels.TierUpgradeOperator,
		labels.DisruptionDestructive, labels.PlatformAny, labels.ComponentOLM,
	},
	PolarionID:                "REPLACE_WITH_POLARION_ID",
	NewUpgradeOperatorFBCTest: newNMOFBCTest,
})
