package sbrparams

import (
	"os"
	"time"
)

const (
	// DefaultPollInterval is the polling interval used with Eventually calls.
	DefaultPollInterval = 5 * time.Second

	// NoNewDaemonSetCheckDuration is how long the controller is observed to confirm no unexpected DaemonSet is created.
	NoNewDaemonSetCheckDuration = 30 * time.Second

	// NoNewDaemonSetCheckInterval is the polling interval used with NoNewDaemonSetCheckDuration.
	NoNewDaemonSetCheckInterval = 5 * time.Second

	// Label represents SBR operator label that can be used for test cases selection.
	Label = "sbr"

	// ExpectedReplicas defines the expected number of replicas for SBR controller manager.
	ExpectedReplicas = int32(2)

	// ManagerContainerName is the name of the main controller container in the SBR pod.
	ManagerContainerName = "manager"

	// CRDGroup is the Kubernetes API group for all SBR custom resources.
	CRDGroup = "storage-based-remediation.medik8s.io"

	// CRDVersion is the API version for all SBR custom resources.
	CRDVersion = "v1alpha1"

	// SBRCTimeoutSecondsMin is the minimum allowed value for sbrTimeoutSeconds (CRD schema enforced).
	SBRCTimeoutSecondsMin = 10

	// SBRCTimeoutSecondsMax is the maximum allowed value for sbrTimeoutSeconds (CRD schema enforced).
	SBRCTimeoutSecondsMax = 300

	// SBRCMaxConsecutiveFailuresMin is the minimum allowed value for maxConsecutiveFailures (CRD schema enforced).
	SBRCMaxConsecutiveFailuresMin = 2

	// SBRCMaxConsecutiveFailuresMax is the maximum allowed value for maxConsecutiveFailures (CRD schema enforced).
	SBRCMaxConsecutiveFailuresMax = 32

	// SBRCInvalidTestName is the name used for short-lived invalid StorageBasedRemediationConfig CRs in negative tests.
	SBRCInvalidTestName = "test-invalid-sbrc"

	// SBRCControllerTestName is the name used for StorageBasedRemediationConfig CRs testing controller-layer validation.
	SBRCControllerTestName = "test-controller-invalid-sbrc"

	// SBRCWatchdogTestName is the name used for StorageBasedRemediationConfig CRs testing invalid watchdog path handling.
	SBRCWatchdogTestName = "test-sbrc-watchdog-neg"

	// SBRCNoMatchSelectorTestName is the name used for StorageBasedRemediationConfig CRs
	// testing non-matching nodeSelector handling.
	SBRCNoMatchSelectorTestName = "test-sbrc-selector-neg"

	// SBRCInvalidWatchdogPath is a watchdog device path guaranteed not to exist on test nodes.
	SBRCInvalidWatchdogPath = "/dev/sbr-test-nonexistent-watchdog"

	// SBRCNoMatchSelectorKey is the node label key used to create a StorageBasedRemediationConfig
	// nodeSelector that matches no nodes.
	SBRCNoMatchSelectorKey = "sbr-test-no-match-selector"

	// SBRCNoMatchSelectorValue is the node label value used to create a StorageBasedRemediationConfig
	// nodeSelector that matches no nodes.
	SBRCNoMatchSelectorValue = "sbr-test-no-match-value"

	// OperatorDeploymentName is the name of the SBR operator controller manager deployment.
	OperatorDeploymentName = "sbr-operator-controller-manager"

	// OperatorControllerPodLabel is the app label value used to select SBR controller pods.
	OperatorControllerPodLabel = "sbr-operator"

	// OperatorControllerPodLabelSelector is the label selector string to filter SBR controller pods,
	// excluding device-init and agent pods that share the app.kubernetes.io/name label.
	OperatorControllerPodLabelSelector = "app.kubernetes.io/name=" + OperatorControllerPodLabel +
		",control-plane=controller-manager"

	// CSVNamePattern is the substring used to match the SBR operator ClusterServiceVersion by name.
	CSVNamePattern = "storage-based-remediation"
)

// WatchdogDebugImage is the container image for /dev/watchdog* discovery pods.
// Must provide sh and ls. Set SBR_WATCHDOG_DEBUG_IMAGE to override (e.g. in disconnected clusters).
var WatchdogDebugImage = func() string {
	if img := os.Getenv("SBR_WATCHDOG_DEBUG_IMAGE"); img != "" {
		return img
	}

	return "registry.access.redhat.com/ubi9/ubi-minimal:latest"
}()
