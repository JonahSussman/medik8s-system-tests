package mdrparams

import "time"

const (
	// DefaultPollInterval is the polling interval used with Eventually calls.
	DefaultPollInterval = 5 * time.Second

	// Label represents mdr operator label that can be used for test cases selection.
	Label = "mdr"

	// ExpectedReplicas defines the expected number of replicas for MDR controller manager.
	ExpectedReplicas = int32(2)

	// ManagerContainerName is the name of the main controller container in the MDR pod.
	ManagerContainerName = "manager"

	// CRDGroup is the Kubernetes API group for all MDR custom resources.
	CRDGroup = "machine-deletion-remediation.medik8s.io"

	// CRDVersion is the API version for all MDR custom resources.
	CRDVersion = "v1alpha1"

	// CSVNamePattern is the substring used to match the MDR operator ClusterServiceVersion by name.
	CSVNamePattern = "machine-deletion-remediation"
)
