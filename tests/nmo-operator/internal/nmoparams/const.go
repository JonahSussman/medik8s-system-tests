package nmoparams

import "time"

const (
	// Label is the operator name used in the suite-level Labels array.
	Label = "nmo"
	// DefaultPollInterval is the polling interval used with Eventually calls.
	DefaultPollInterval = 5 * time.Second

	// Label represents nmo operator label that can be used for test cases selection.

	// ExpectedReplicas defines the expected number of replicas for NMO controller manager.
	// NMO always runs a single replica regardless of cluster topology (MNO or SNO).
	ExpectedReplicas = int32(1)

	// ManagerContainerName is the name of the main controller container in the NMO pod.
	ManagerContainerName = "manager"

	// OperatorDeploymentName is the name of the NMO operator controller manager deployment.
	OperatorDeploymentName = "node-maintenance-operator-controller-manager"

	// OperatorControllerPodLabelSelector is the label selector string to filter NMO controller pods.
	// NMO's upstream pod template only defines control-plane=controller-manager (no app.kubernetes.io/name).
	OperatorControllerPodLabelSelector = "control-plane=controller-manager"

	// CSVNamePattern is the substring used to match the NMO operator ClusterServiceVersion by name.
	CSVNamePattern = "node-maintenance-operator"
)
