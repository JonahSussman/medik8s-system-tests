package nhcparams

import (
	"fmt"
	"os"
	"regexp"
)

const (
	// UpgradeSubName selects the NHC upgrade scenario.
	UpgradeSubName = "nhc-operator-upgrade"
	// NHCUpgradeTestName is the fixed name of the test-owned NodeHealthCheck.
	NHCUpgradeTestName = "nhc-operator-upgrade"
	// NHCUpgradeTemplateName is the fixed name of the test-owned remediation template.
	NHCUpgradeTemplateName = "nhc-operator-upgrade-template"
)

// UpgradeInputs make the exact input artifacts visible to both local and CI
// runs. Required values intentionally have no floating-image defaults.
type UpgradeInputs struct {
	OldBundle, OldVersion, OldImage                        string
	CandidateBundle, CandidateVersion, CandidateImage      string
	CandidateCommit, TestRevision                          string
	SNRBundle, SNRPackage, Package, Namespace, OperatorSDK string
}

// LoadUpgradeInputs reads and validates the pinned artifacts for an upgrade run.
func LoadUpgradeInputs() (UpgradeInputs, error) {
	inputs := UpgradeInputs{
		OldBundle: os.Getenv("NHC_UPGRADE_OLD_BUNDLE"), OldVersion: os.Getenv("NHC_UPGRADE_OLD_VERSION"),
		OldImage: os.Getenv("NHC_UPGRADE_OLD_IMAGE"), CandidateBundle: os.Getenv("NHC_UPGRADE_CANDIDATE_BUNDLE"),
		CandidateVersion: os.Getenv("NHC_UPGRADE_CANDIDATE_VERSION"),
		CandidateImage:   os.Getenv("NHC_UPGRADE_CANDIDATE_IMAGE"),
		CandidateCommit:  os.Getenv("NHC_UPGRADE_CANDIDATE_COMMIT"), TestRevision: os.Getenv("NHC_UPGRADE_TEST_REVISION"),
		SNRBundle: os.Getenv("NHC_UPGRADE_SNR_BUNDLE"), SNRPackage: os.Getenv("NHC_UPGRADE_SNR_PACKAGE"),
		Package: os.Getenv("NHC_UPGRADE_PACKAGE"), Namespace: os.Getenv("NHC_UPGRADE_NAMESPACE"),
		OperatorSDK: os.Getenv("NHC_UPGRADE_OPERATOR_SDK"),
	}
	for key, value := range map[string]string{
		"NHC_UPGRADE_OLD_BUNDLE": inputs.OldBundle, "NHC_UPGRADE_OLD_VERSION": inputs.OldVersion,
		"NHC_UPGRADE_OLD_IMAGE": inputs.OldImage, "NHC_UPGRADE_CANDIDATE_BUNDLE": inputs.CandidateBundle,
		"NHC_UPGRADE_CANDIDATE_VERSION": inputs.CandidateVersion, "NHC_UPGRADE_CANDIDATE_IMAGE": inputs.CandidateImage,
		"NHC_UPGRADE_CANDIDATE_COMMIT": inputs.CandidateCommit, "NHC_UPGRADE_TEST_REVISION": inputs.TestRevision,
		"NHC_UPGRADE_SNR_BUNDLE": inputs.SNRBundle, "NHC_UPGRADE_SNR_PACKAGE": inputs.SNRPackage,
		"NHC_UPGRADE_PACKAGE": inputs.Package, "NHC_UPGRADE_NAMESPACE": inputs.Namespace,
		"NHC_UPGRADE_OPERATOR_SDK": inputs.OperatorSDK,
	} {
		if value == "" {
			return UpgradeInputs{}, fmt.Errorf("%s must be set for the NHC operator upgrade scenario", key)
		}
	}

	if inputs.Package != "node-healthcheck-operator" || inputs.SNRPackage != "self-node-remediation" {
		return UpgradeInputs{}, fmt.Errorf("this sample requires the upstream NHC and SNR packages")
	}

	if inputs.OldVersion == inputs.CandidateVersion || inputs.OldImage == inputs.CandidateImage {
		return UpgradeInputs{}, fmt.Errorf("candidate version and image must differ from the old installation")
	}

	commit := regexp.MustCompile(`^[a-f0-9]{40}$`)
	if !commit.MatchString(inputs.CandidateCommit) || !commit.MatchString(inputs.TestRevision) {
		return UpgradeInputs{}, fmt.Errorf("candidate and test revisions must be full Git commit hashes")
	}

	return inputs, nil
}
