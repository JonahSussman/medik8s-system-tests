package snrparams

import "time"

const (
	// Label is the operator name used in the suite-level Labels array.
	Label = "snr"
	// DefaultPollInterval is the polling interval used with Eventually calls.
	DefaultPollInterval = 5 * time.Second

	// ExpectedReplicas defines the expected number of replicas for SNR controller manager.
	ExpectedReplicas = int32(2)

	// CRDGroup is the Kubernetes API group for all SNR custom resources.
	CRDGroup = "self-node-remediation.medik8s.io"

	// CRDVersion is the API version for all SNR custom resources.
	CRDVersion = "v1alpha1"

	// SNRConfigName is the name of the default SelfNodeRemediationConfig CR.
	SNRConfigName = "self-node-remediation-config"

	// SNRTemplateName is the name of the default automatic strategy template.
	SNRTemplateName = "self-node-remediation-automatic-strategy-template"

	// SNRTemplateExpectedStrategy is the expected remediation strategy for the automatic template.
	SNRTemplateExpectedStrategy = "Automatic"

	// CSVNamePattern is the CSV name pattern used to find the SNR ClusterServiceVersion.
	CSVNamePattern = "self-node-remediation"

	// SNRCRDName is the full CRD name for SelfNodeRemediationConfig.
	SNRCRDName = "selfnoderemediationconfigs.self-node-remediation.medik8s.io"

	// SafeTimeToAssumeNodeRebootedDescription is the expected description
	// text substring for the safeTimeToAssumeNodeRebootedSeconds CRD field.
	SafeTimeToAssumeNodeRebootedDescription = "SafeTimeToAssumeNodeRebootedSeconds " +
		"is the time after which the healthy self node remediation"

	// SNRCNonDefaultErrMsg is the expected error when creating a non-default SNRC.
	SNRCNonDefaultErrMsg = "to enforce only one SelfNodeRemediationConfig " +
		"in the cluster, a name other than " +
		"self-node-remediation-config is not allowed"

	// SNRTestNodeName is the fake node name used in negative tests.
	SNRTestNodeName = "incorrect-node-name"

	// SNRCTestName is the name used for test SNRC CRs.
	SNRCTestName = "test-snrc"
)
