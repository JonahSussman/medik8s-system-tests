package sbrparams

import (
	"fmt"
	"os"
)

// FBCRemediationInputs controls the optional, destructive post-upgrade check.
type FBCRemediationInputs struct {
	Enabled      bool   `json:"enabled"`
	StorageClass string `json:"storageClass,omitempty"`
}

// LoadFBCRemediationInputs defaults to an upgrade/configuration-only run without storage.
func LoadFBCRemediationInputs() (FBCRemediationInputs, error) {
	value := os.Getenv("SBR_FBC_REMEDIATION")
	if value != "" && value != "false" && value != "true" {
		return FBCRemediationInputs{}, fmt.Errorf("SBR_FBC_REMEDIATION must be true or false")
	}

	inputs := FBCRemediationInputs{Enabled: value == "true"}
	if inputs.Enabled {
		inputs.StorageClass = os.Getenv("SBR_STORAGE_CLASS")
		if inputs.StorageClass == "" {
			return FBCRemediationInputs{}, fmt.Errorf(
				"SBR_STORAGE_CLASS must name an existing RWX class when SBR_FBC_REMEDIATION=true")
		}
	}

	return inputs, nil
}
