package medik8sparams

import (
	"time"
)

const (
	// Label represents medik8s label that can be used for test cases selection.
	Label = "medik8s"
	// OperatorNs custom namespace of medik8s operators.
	OperatorNs = "openshift-workload-availability"
	// DefaultTimeout represents the default timeout.
	DefaultTimeout = 300 * time.Second

	// DefaultWorkloadImage is the container image used for test workload pods.
	DefaultWorkloadImage = "image-registry.openshift-image-registry.svc:5000/openshift/tools:latest"
)
