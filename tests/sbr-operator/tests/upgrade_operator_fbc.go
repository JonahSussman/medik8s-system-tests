package tests

import (
	"crypto/rand"
	"os"
	"strings"

	"github.com/medik8s/system-tests/tests/internal/fbcsuite"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrutils"
)

func newSBRFBCTest(inputs medik8sparams.FBCUpgradeInputs) fbcsuite.UpgradeOperatorFBCTest {
	return &sbrUpgradeOperatorFBCTest{
		inputs: inputs,
		owned: &sbrutils.OwnedRun{
			API: APIClient, Namespace: medik8sparams.OperatorNs, Token: strings.ToLower(rand.Text()),
		},
	}
}

func sbrFBCLabels() []string {
	suiteLabels := []string{
		labels.OperatorSBR, sbrparams.Label, labels.TierUpgradeOperator,
		labels.PlatformAny, labels.ComponentOLM,
	}
	if os.Getenv("SBR_FBC_REMEDIATION") == "true" {
		return append(suiteLabels, labels.DisruptionDestructive, labels.ComponentRemediation)
	}

	return append(suiteLabels, labels.DisruptionNonDestructive)
}

var _ = fbcsuite.DefineFBCUpgradeSuite(fbcsuite.UpgradeOperatorFBCConfig{
	OperatorName:              "SBR",
	PackageName:               sbrparams.UpgradeSBRPackage,
	SubscriptionName:          "sbr-operator-upgrade-sub",
	CSVNamePattern:            sbrparams.CSVNamePattern,
	DeploymentName:            sbrparams.OperatorDeploymentName,
	ContainerName:             sbrparams.ManagerContainerName,
	Labels:                    sbrFBCLabels(),
	PolarionID:                "REPLACE_WITH_POLARION_ID",
	NewUpgradeOperatorFBCTest: newSBRFBCTest,
})
