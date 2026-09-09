package nhcparams

import (
	"fmt"
	"os"
)

const (
	UpgradeSubName         = "nhc-operator-upgrade"
	NHCUpgradeTestName     = "nhc-operator-upgrade"
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

func LoadUpgradeInputs() (UpgradeInputs, error) {
	inputs := UpgradeInputs{
		OldBundle: os.Getenv("NHC_UPGRADE_OLD_BUNDLE"), OldVersion: os.Getenv("NHC_UPGRADE_OLD_VERSION"),
		OldImage: os.Getenv("NHC_UPGRADE_OLD_IMAGE"), CandidateBundle: os.Getenv("NHC_UPGRADE_CANDIDATE_BUNDLE"),
		CandidateVersion: os.Getenv("NHC_UPGRADE_CANDIDATE_VERSION"), CandidateImage: os.Getenv("NHC_UPGRADE_CANDIDATE_IMAGE"),
		CandidateCommit: os.Getenv("NHC_UPGRADE_CANDIDATE_COMMIT"), TestRevision: os.Getenv("NHC_UPGRADE_TEST_REVISION"),
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
	return inputs, nil
}
