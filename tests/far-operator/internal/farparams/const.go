package farparams

import "time"

const (
	// DefaultPollInterval is the polling interval used with Eventually calls.
	DefaultPollInterval = 5 * time.Second

	// Label represents far operator label that can be used for test cases selection.
	Label = "far"

	// ExpectedReplicas defines the expected number of replicas for FAR controller manager.
	ExpectedReplicas = int32(2)

	// ManagerContainerName is the name of the main controller container in the FAR pod.
	ManagerContainerName = "manager"
)
