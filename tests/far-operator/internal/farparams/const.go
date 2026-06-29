package farparams

import "time"

const (
	// Label is the operator name used in the suite-level Labels array.
	Label = "far"
	// DefaultPollInterval is the polling interval used with Eventually calls.
	DefaultPollInterval = 5 * time.Second

	// ExpectedReplicas defines the expected number of replicas for FAR controller manager.
	ExpectedReplicas = int32(2)

	// ManagerContainerName is the name of the main controller container in the FAR pod.
	ManagerContainerName = "manager"

	// FenceAgentsRemediationCRDName is the full CRD name for FenceAgentsRemediation.
	FenceAgentsRemediationCRDName = "fenceagentsremediations.fence-agents-remediation.medik8s.io"
	// FenceAgentsRemediationTemplateCRDName is the full CRD name for FenceAgentsRemediationTemplate.
	FenceAgentsRemediationTemplateCRDName = "fenceagentsremediationtemplates.fence-agents-remediation.medik8s.io"

	// PSAEnforceLabelKey is the Pod Security Admission enforcement label key.
	PSAEnforceLabelKey = "pod-security.kubernetes.io/enforce"
	// PSAExpectedLevel is the expected PSA enforcement level for the operator namespace.
	PSAExpectedLevel = "privileged"

	// ExpectedPriorityClassName is the priorityClassName that FAR controller pods must have.
	ExpectedPriorityClassName = "system-cluster-critical"

	// ControllerPodLabelKey is the standard K8s label key for the FAR controller pod.
	ControllerPodLabelKey = "app.kubernetes.io/name"

	// FenceAgentBinaryPrefix is the filename prefix for fence agent binaries in /usr/sbin.
	FenceAgentBinaryPrefix = "fence_"
)
