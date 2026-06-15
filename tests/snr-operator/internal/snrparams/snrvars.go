package snrparams

import (
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/openshift-kni/k8sreporter"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// Labels represents the range of labels that can be used for test cases selection.
	Labels = []string{medik8sparams.Label, Label}

	// OperatorDeploymentName represents SNR deployment name.
	OperatorDeploymentName = "self-node-remediation-controller-manager"

	// OperatorControllerPodLabelSelector selects SNR controller-manager pods.
	// The trailing "=" matches the empty-value label that SNR controller pods carry:
	//   self-node-remediation-operator: ""
	OperatorControllerPodLabelSelector = "control-plane=controller-manager,self-node-remediation-operator="

	// DaemonSetPodLabelSelector selects SNR DaemonSet agent pods by their labels:
	//   app.kubernetes.io/name=self-node-remediation, app.kubernetes.io/component=agent
	DaemonSetPodLabelSelector = "app.kubernetes.io/name=self-node-remediation,app.kubernetes.io/component=agent"

	operatorNs = medik8sparams.OperatorNs

	// ReporterNamespacesToDump tells the reporter from where to collect logs.
	ReporterNamespacesToDump = map[string]string{
		medik8sparams.OperatorNs: medik8sparams.OperatorNs,
		"openshift-machine-api":  "openshift-machine-api",
	}

	// ReporterCRDsToDump tells the reporter what CRs to dump.
	ReporterCRDsToDump = []k8sreporter.CRData{
		{Cr: &corev1.PodList{}},
		{Cr: newUnstructuredList(CRDGroup, CRDVersion, "SelfNodeRemediationList")},
		{Cr: newUnstructuredList(CRDGroup, CRDVersion, "SelfNodeRemediationConfigList")},
		{Cr: newUnstructuredList(CRDGroup, CRDVersion, "SelfNodeRemediationTemplateList")},
		{Cr: &coordinationv1.LeaseList{}, Namespace: &operatorNs},
	}

	// RequiredAnnotations defines the required annotations and expected values for SNR CSV.
	RequiredAnnotations = map[string]string{
		"features.operators.openshift.io/tls-profiles":     "false",
		"features.operators.openshift.io/disconnected":     "true",
		"features.operators.openshift.io/fips-compliant":   "true",
		"features.operators.openshift.io/proxy-aware":      "false",
		"features.operators.openshift.io/cnf":              "false",
		"features.operators.openshift.io/cni":              "false",
		"features.operators.openshift.io/csi":              "false",
		"features.operators.openshift.io/token-auth-aws":   "false",
		"features.operators.openshift.io/token-auth-azure": "false",
		"features.operators.openshift.io/token-auth-gcp":   "false",
		"operatorframework.io/suggested-namespace":         medik8sparams.OperatorNs,
	}

	// UnsupportedTemplateNames lists template names that must NOT exist.
	UnsupportedTemplateNames = []string{
		"self-node-remediation-resource-deletion-template",
		"self-node-remediation-node-deletion-template",
	}
)

func newUnstructuredList(group, version, kind string) *unstructured.UnstructuredList {
	l := &unstructured.UnstructuredList{}
	l.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind})

	return l
}
