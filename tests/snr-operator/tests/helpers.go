package tests

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"

	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/snr-operator/internal/snrparams"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// buildSNRCR builds an unstructured SNR custom resource of the given kind.
func buildSNRCR(kind, name string, spec map[string]interface{}) *unstructured.Unstructured {
	resource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": snrparams.CRDGroup + "/" + snrparams.CRDVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": medik8sparams.OperatorNs,
			},
		},
	}

	if spec != nil {
		resource.Object["spec"] = spec
	}

	return resource
}

// buildSNRWithAnnotations creates an SNR CR with optional annotations.
func buildSNRWithAnnotations(
	name string, annotations map[string]string,
) *unstructured.Unstructured {
	metadata := map[string]interface{}{
		"name":      name,
		"namespace": medik8sparams.OperatorNs,
	}

	if annotations != nil {
		annotationMap := make(map[string]interface{}, len(annotations))
		for key, val := range annotations {
			annotationMap[key] = val
		}

		metadata["annotations"] = annotationMap
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": snrparams.CRDGroup + "/" + snrparams.CRDVersion,
			"kind":       "SelfNodeRemediation",
			"metadata":   metadata,
		},
	}
}

// deferDeleteCR registers cleanup for a CR, retrying deletion with Eventually.
func deferDeleteCR(resource *unstructured.Unstructured) {
	DeferCleanup(func() {
		Eventually(func() error {
			deleteErr := APIClient.Delete(context.TODO(), resource)
			if k8serrors.IsNotFound(deleteErr) {
				return nil
			}

			return deleteErr
		}, medik8sparams.DefaultTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
			"cleanup of test CR %q must succeed", resource.GetName())
	})
}

// snrGVK returns the GVK for SelfNodeRemediation.
func snrGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   snrparams.CRDGroup,
		Version: snrparams.CRDVersion,
		Kind:    "SelfNodeRemediation",
	}
}

// snrcGVK returns the GVK for SelfNodeRemediationConfig.
func snrcGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   snrparams.CRDGroup,
		Version: snrparams.CRDVersion,
		Kind:    "SelfNodeRemediationConfig",
	}
}

// snrcForPatch returns a minimal unstructured object suitable for client.Patch calls.
func snrcForPatch(name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(snrcGVK())
	obj.SetName(name)
	obj.SetNamespace(medik8sparams.OperatorNs)

	return obj
}

// verifyDSPodsRunning checks that SNR DaemonSet pods exist and are all Running
// with ready containers.
func verifyDSPodsRunning() error {
	dsListOptions := metav1.ListOptions{
		LabelSelector: snrparams.DaemonSetPodLabelSelector,
	}

	dsPods, listErr := pod.List(APIClient, medik8sparams.OperatorNs, dsListOptions)
	if listErr != nil {
		return fmt.Errorf("failed to list SNR DaemonSet pods: %w", listErr)
	}

	if len(dsPods) == 0 {
		return fmt.Errorf("no SNR DaemonSet pods found")
	}

	for _, dsPod := range dsPods {
		if dsPod.Object.Status.Phase != corev1.PodRunning {
			return fmt.Errorf("SNR DaemonSet pod %q is %s, expected Running",
				dsPod.Object.Name, dsPod.Object.Status.Phase)
		}

		for _, cs := range dsPod.Object.Status.ContainerStatuses {
			if !cs.Ready {
				return fmt.Errorf("SNR DaemonSet pod %q container %q is not ready",
					dsPod.Object.Name, cs.Name)
			}
		}
	}

	return nil
}

// verifyDSPodsGone checks that no SNR DaemonSet pods exist.
func verifyDSPodsGone() error {
	dsListOptions := metav1.ListOptions{
		LabelSelector: snrparams.DaemonSetPodLabelSelector,
	}

	dsPods, listErr := pod.List(APIClient, medik8sparams.OperatorNs, dsListOptions)
	if listErr != nil {
		return fmt.Errorf("failed to list DS pods: %w", listErr)
	}

	if len(dsPods) > 0 {
		return fmt.Errorf("still %d SNR DS pods running, expected 0", len(dsPods))
	}

	return nil
}

// findMessageInDSPodLogs searches SNR DS pod logs from the last logWindow
// for the given message. Returns nil when found in at least one pod.
func findMessageInDSPodLogs(message string, logWindow time.Duration) error {
	dsListOptions := metav1.ListOptions{
		LabelSelector: snrparams.DaemonSetPodLabelSelector,
	}

	dsPods, listErr := pod.List(APIClient, medik8sparams.OperatorNs, dsListOptions)
	if listErr != nil {
		return fmt.Errorf("failed to list SNR DaemonSet pods: %w", listErr)
	}

	if len(dsPods) == 0 {
		return fmt.Errorf("no SNR DaemonSet pods found")
	}

	for _, dsPod := range dsPods {
		logStr, logErr := dsPod.GetLog(logWindow, "")
		if logErr != nil {
			continue
		}

		if strings.Contains(logStr, message) {
			return nil
		}
	}

	return fmt.Errorf("message %q not found in any SNR DS pod logs (last %s)",
		message, logWindow)
}
