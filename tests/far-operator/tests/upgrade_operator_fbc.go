package tests

import (
	"crypto/rand"
	"os"
	"strings"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
	"github.com/medik8s/system-tests/tests/far-operator/internal/farutils"
	"github.com/medik8s/system-tests/tests/internal/fbcsuite"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
)

func newFARFBCTest(inputs medik8sparams.FBCUpgradeInputs) fbcsuite.UpgradeOperatorFBCTest {
	return &farUpgradeOperatorFBCTest{
		inputs: inputs,
		owned: &helpers.FBCNamespace{
			API: APIClient, Name: medik8sparams.OperatorNs, Token: strings.ToLower(rand.Text()),
		},
	}
}

func farFBCLabels() []string {
	result := []string{
		labels.OperatorFAR, farparams.Label, labels.TierUpgradeOperator, labels.ComponentOLM,
	}
	// Setup reports invalid values before acquiring any resources.
	enabled, _ := farutils.FBCRemediationEnabled(os.Getenv("FAR_FBC_REMEDIATION"))
	if enabled {
		return append(result, labels.DisruptionDestructive, labels.PlatformAWS, labels.ComponentRemediation)
	}

	return append(result, labels.DisruptionNonDestructive, labels.PlatformAny)
}

var _ = fbcsuite.DefineFBCUpgradeSuite(fbcsuite.UpgradeOperatorFBCConfig{
	OperatorName:              "FAR",
	PackageName:               "fence-agents-remediation",
	SubscriptionName:          farparams.UpgradeSubName,
	CSVNamePattern:            "fence-agents-remediation",
	DeploymentName:            farparams.OperatorDeploymentName,
	ContainerName:             farparams.ManagerContainerName,
	Labels:                    farFBCLabels(),
	PolarionID:                "REPLACE_WITH_POLARION_ID",
	NewUpgradeOperatorFBCTest: newFARFBCTest,
})
