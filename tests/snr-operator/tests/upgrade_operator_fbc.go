package tests

import (
	"crypto/rand"
	"strings"

	"github.com/medik8s/system-tests/tests/internal/fbcsuite"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/snr-operator/internal/snrparams"
)

func newSNRFBCTest(inputs medik8sparams.FBCUpgradeInputs) fbcsuite.UpgradeOperatorFBCTest {
	return &snrUpgradeOperatorFBCTest{
		inputs: inputs,
		owned: &helpers.FBCNamespace{
			API: APIClient, Name: medik8sparams.OperatorNs, Token: strings.ToLower(rand.Text()),
		},
	}
}

var _ = fbcsuite.DefineFBCUpgradeSuite(fbcsuite.UpgradeOperatorFBCConfig{
	OperatorName:     "SNR",
	PackageName:      "self-node-remediation",
	SubscriptionName: "snr-operator-upgrade-sub",
	CSVNamePattern:   snrparams.CSVNamePattern,
	DeploymentName:   snrparams.OperatorDeploymentName,
	ContainerName:    snrparams.ManagerContainerName,
	Labels: []string{
		labels.OperatorSNR, snrparams.Label, labels.TierUpgradeOperator,
		labels.DisruptionDestructive, labels.PlatformAny, labels.ComponentOLM, labels.ComponentRemediation,
	},
	PolarionID:                "REPLACE_WITH_POLARION_ID",
	NewUpgradeOperatorFBCTest: newSNRFBCTest,
})
