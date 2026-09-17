package nhcparams

import (
	"strings"
	"testing"
)

func setRequiredCandidateInputs(t *testing.T) {
	t.Helper()

	t.Setenv("NHC_UPGRADE_CANDIDATE_NHC_BUNDLE", "registry.test/nhc-bundle:candidate")
	t.Setenv("NHC_UPGRADE_CANDIDATE_NHC_VERSION", "5.8.0")
	t.Setenv("NHC_UPGRADE_CANDIDATE_NHC_IMAGE", "registry.test/nhc:candidate")
	t.Setenv("NHC_UPGRADE_OPERATOR_SDK", "/test/operator-sdk")
	t.Setenv("NHC_UPGRADE_TEST_REVISION", strings.Repeat("a", 40))
}

func TestLoadUpgradeOperatorInputsUsesFixedIdentity(t *testing.T) {
	setRequiredCandidateInputs(t)
	t.Setenv("NHC_UPGRADE_PACKAGE", "ignored-nhc")
	t.Setenv("NHC_UPGRADE_SNR_PACKAGE", "ignored-snr")
	t.Setenv("NHC_UPGRADE_NAMESPACE", "ignored-namespace")

	inputs, err := LoadUpgradeOperatorInputs()
	if err != nil {
		t.Fatal(err)
	}

	if inputs.Package != UpgradeNHCPackage || inputs.SNRPackage != UpgradeSNRPackage ||
		inputs.Namespace != UpgradeNamespace {
		t.Fatalf("upgrade identity is not fixed: %+v", inputs)
	}

	if inputs.BaselineNHC.Bundle != "" || inputs.BaselineSNR.Bundle != "" || inputs.CandidateSNR.Bundle != "" {
		t.Fatalf("optional artifacts unexpectedly defaulted before resolution: %+v", inputs)
	}
}

func TestLoadUpgradeClusterInputsUsesRenamedVariables(t *testing.T) {
	setRequiredCandidateInputs(t)

	inputs, err := LoadUpgradeClusterInputs()
	if err != nil {
		t.Fatal(err)
	}

	if inputs.CandidateNHC.Bundle != "registry.test/nhc-bundle:candidate" || inputs.CandidateNHC.Version != "5.8.0" ||
		inputs.CandidateNHC.Image != "registry.test/nhc:candidate" || inputs.Package != UpgradeNHCPackage ||
		inputs.Namespace != UpgradeNamespace {
		t.Fatalf("unexpected candidate inputs: %+v", inputs)
	}
}

func TestLoadFreshInstallInputsContainsOnlyCandidateArtifacts(t *testing.T) {
	setRequiredCandidateInputs(t)

	inputs, err := LoadFreshInstallInputs()
	if err != nil {
		t.Fatal(err)
	}

	if inputs.CandidateNHC.Bundle != "registry.test/nhc-bundle:candidate" || inputs.CandidateSNR.Bundle != "" ||
		inputs.Package != UpgradeNHCPackage || inputs.SNRPackage != UpgradeSNRPackage ||
		inputs.Namespace != UpgradeNamespace {
		t.Fatalf("unexpected fresh-install inputs: %+v", inputs)
	}
}

func TestLoadUpgradeOperatorInputsRequiresOnlyCandidateNHCAndSDK(t *testing.T) {
	setRequiredCandidateInputs(t)

	for _, variable := range []string{
		"NHC_UPGRADE_CANDIDATE_NHC_BUNDLE",
		"NHC_UPGRADE_CANDIDATE_NHC_VERSION",
		"NHC_UPGRADE_CANDIDATE_NHC_IMAGE",
		"NHC_UPGRADE_OPERATOR_SDK",
	} {
		t.Run(variable, func(t *testing.T) {
			setRequiredCandidateInputs(t)
			t.Setenv(variable, "")

			if _, err := LoadUpgradeOperatorInputs(); err == nil || !strings.Contains(err.Error(), variable) {
				t.Fatalf("missing %s was not reported: %v", variable, err)
			}
		})
	}
}
