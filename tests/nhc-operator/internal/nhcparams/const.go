package nhcparams

import "time"

const (
	// DefaultPollInterval is the polling interval used with Eventually calls.
	DefaultPollInterval = 5 * time.Second

	// Label represents nhc operator label that can be used for test cases selection.
	Label = "nhc"

	// ExpectedReplicas defines the expected number of replicas for NHC controller manager.
	ExpectedReplicas = int32(2)

	// CRDGroup is the Kubernetes API group for NHC custom resources.
	CRDGroup = "remediation.medik8s.io"

	// CRDVersion is the API version for NHC custom resources.
	CRDVersion = "v1alpha1"

	// CSVNamePattern is the CSV name pattern used to find the NHC ClusterServiceVersion.
	CSVNamePattern = "node-healthcheck-operator"
)
