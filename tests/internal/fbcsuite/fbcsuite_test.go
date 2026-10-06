package fbcsuite

import (
	"testing"

	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
)

func TestUpgradeOperatorFBCConfigValidation(t *testing.T) {
	for _, test := range []struct {
		name       string
		deployment string
		container  string
		wantPanic  bool
	}{
		{name: "both empty"},
		{name: "both set", deployment: "controller", container: "manager"},
		{name: "deployment only", deployment: "controller", wantPanic: true},
		{name: "container only", container: "manager", wantPanic: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := UpgradeOperatorFBCConfig{
				DeploymentName: test.deployment,
				ContainerName:  test.container,
				NewUpgradeOperatorFBCTest: func(medik8sparams.FBCUpgradeInputs) UpgradeOperatorFBCTest {
					return nil
				},
			}
			defer func() {
				failure := recover()
				if (failure != nil) != test.wantPanic {
					t.Fatalf("unexpected validation panic: %v", failure)
				}
			}()
			cfg.validate()
		})
	}
}
