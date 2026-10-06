package sbrparams

import "testing"

func TestLoadFBCRemediationInputs(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		storage string
		enabled bool
		wantErr bool
	}{
		{name: "default"},
		{name: "default ignores storage", storage: "existing-rwx"},
		{name: "disabled", value: "false", storage: "existing-rwx"},
		{name: "enabled", value: "true", storage: "existing-rwx", enabled: true},
		{name: "missing storage", value: "true", wantErr: true},
		{name: "invalid boolean", value: "yes", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SBR_FBC_REMEDIATION", test.value)
			t.Setenv("SBR_STORAGE_CLASS", test.storage)
			inputs, err := LoadFBCRemediationInputs()
			if (err != nil) != test.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}

			if test.wantErr {
				return
			}

			if inputs.Enabled != test.enabled || (!test.enabled && inputs.StorageClass != "") {
				t.Fatalf("unexpected remediation inputs: %+v", inputs)
			}

			if test.enabled && inputs.StorageClass != test.storage {
				t.Fatalf("lost storage class: %+v", inputs)
			}
		})
	}
}
