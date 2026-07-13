package tests

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/mdr-operator/internal/mdrparams"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mdrGVK is the GroupVersionKind for MachineDeletionRemediation CRs.
var mdrGVK = schema.GroupVersionKind{
	Group:   mdrparams.CRDGroup,
	Version: mdrparams.CRDVersion,
	Kind:    "MachineDeletionRemediation",
}

// mdrtGVK is the GroupVersionKind for MachineDeletionRemediationTemplate CRs.
var mdrtGVK = schema.GroupVersionKind{
	Group:   mdrparams.CRDGroup,
	Version: mdrparams.CRDVersion,
	Kind:    "MachineDeletionRemediationTemplate",
}

// nhcGVK is the GroupVersionKind for NodeHealthCheck CRs.
var nhcGVK = schema.GroupVersionKind{
	Group:   mdrparams.NHCAPIGroup,
	Version: mdrparams.NHCAPIVersion,
	Kind:    "NodeHealthCheck",
}

// buildMDRT builds an unstructured MachineDeletionRemediationTemplate CR.
func buildMDRT(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": mdrparams.CRDGroup + "/" + mdrparams.CRDVersion,
			"kind":       "MachineDeletionRemediationTemplate",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": medik8sparams.OperatorNs,
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{},
				},
			},
		},
	}
}

// buildNHCForMDR builds an unstructured NodeHealthCheck CR that monitors
// worker nodes and triggers MDR remediation via the named MDRT.
func buildNHCForMDR(name, mdrtName string) *unstructured.Unstructured {
	nhc := &unstructured.Unstructured{}
	nhc.SetGroupVersionKind(nhcGVK)
	nhc.SetName(name)

	nhc.Object["spec"] = map[string]interface{}{
		"selector": map[string]interface{}{
			"matchExpressions": []interface{}{
				map[string]interface{}{
					"key":      "node-role.kubernetes.io/worker",
					"operator": "Exists",
				},
			},
		},
		"remediationTemplate": map[string]interface{}{
			"apiVersion": mdrparams.CRDGroup + "/" + mdrparams.CRDVersion,
			"kind":       "MachineDeletionRemediationTemplate",
			"name":       mdrtName,
			"namespace":  medik8sparams.OperatorNs,
		},
		"minHealthy": int64(1),
		"unhealthyConditions": []interface{}{
			map[string]interface{}{
				"type":     "Ready",
				"status":   "False",
				"duration": "60s",
			},
			map[string]interface{}{
				"type":     "Ready",
				"status":   "Unknown",
				"duration": "60s",
			},
		},
	}

	return nhc
}

// isNHCCRDInstalled checks whether the NodeHealthCheck CRD is registered
// in the cluster. Returns false only for genuine NotFound; fails the test
// on transient API errors to avoid silently skipping tests.
func isNHCCRDInstalled() bool {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := APIClient.Get(
		context.TODO(),
		types.NamespacedName{Name: mdrparams.NHCCRDName},
		crd,
	)

	if err == nil {
		return true
	}

	if k8serrors.IsNotFound(err) {
		return false
	}

	Fail(fmt.Sprintf("isNHCCRDInstalled: unexpected error checking CRD %s: %v",
		mdrparams.NHCCRDName, err))

	return false
}

// stopKubeletForRemediation wraps helpers.StopKubelet with error
// suppression for expected failure modes during kubelet stop.
func stopKubeletForRemediation(ctx context.Context, nodeName string) error {
	err := helpers.StopKubelet(ctx, nodeName, mdrparams.OcDebugTimeout)
	if err == nil {
		return nil
	}

	errMsg := err.Error()

	if strings.Contains(errMsg, "oc debug on node") && strings.Contains(errMsg, "timed out") ||
		strings.Contains(errMsg, "unable to create the debug pod") ||
		(strings.Contains(errMsg, "exit status 1") && strings.Contains(errMsg, "Starting pod")) {
		GinkgoWriter.Printf(
			"stopKubeletForRemediation(%s): suppressed expected error "+
				"(kubelet likely stopped): %v\n", nodeName, err)

		return nil
	}

	return err
}

// deleteRemediationCR performs a retry-safe deletion of an unstructured
// CR. Follows the same get-delete-confirm pattern as SNR and FAR.
func deleteRemediationCR(
	ctx context.Context, k8sClient client.Client,
	gvk schema.GroupVersionKind, name string,
) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	key := types.NamespacedName{
		Name:      name,
		Namespace: medik8sparams.OperatorNs,
	}

	if waitErr := wait.PollUntilContextTimeout(
		ctx, mdrparams.DefaultPollInterval,
		mdrparams.RemediationCRDeletionTimeout, true,
		func(ctx context.Context) (bool, error) {
			if err := k8sClient.Get(ctx, key, obj); err != nil {
				if k8serrors.IsNotFound(err) {
					return true, nil
				}

				return false, nil
			}

			if delErr := k8sClient.Delete(ctx, obj); delErr != nil {
				if k8serrors.IsNotFound(delErr) {
					return true, nil
				}

				return false, nil
			}

			return false, nil
		},
	); waitErr != nil {
		GinkgoWriter.Printf(
			"Warning: %s %s not fully deleted within %s: %v\n",
			gvk.Kind, name, mdrparams.RemediationCRDeletionTimeout, waitErr)
	}
}

// cleanupNHCCR safely deletes a NodeHealthCheck CR by name.
func cleanupNHCCR(name string) {
	deleteRemediationCR(
		context.TODO(), APIClient, nhcGVK, name)
}

// cleanupMDRT safely deletes a MachineDeletionRemediationTemplate CR by name.
func cleanupMDRT(name string) {
	deleteRemediationCR(
		context.TODO(), APIClient, mdrtGVK, name)
}

// cleanupMDRCR safely deletes a MachineDeletionRemediation CR by name.
func cleanupMDRCR(name string) {
	deleteRemediationCR(
		context.TODO(), APIClient, mdrGVK, name)
}

// waitForMDRRemediationComplete polls until MDR remediation finishes.
// Unlike SNR/SBR (where the node reboots and keeps its name), MDR deletes
// the Machine and the cloud provisions a new VM. The replacement node
// typically has a DIFFERENT name (new EC2 instance = new hostname).
//
// Success is defined as: MDR CR for the original node is gone AND a new
// worker node appears that was NOT in the initialWorkerNames set.
// Returns the name of the replacement node.
func waitForMDRRemediationComplete(
	ctx context.Context, originalNodeName string,
	expectedWorkerCount int, initialWorkerNames map[string]bool,
	timeout time.Duration,
) (string, error) {
	var newNodeName string

	err := wait.PollUntilContextTimeout(
		ctx, mdrparams.DefaultPollInterval, timeout, true,
		func(ctx context.Context) (bool, error) {
			// Check if MDR CR still exists.
			mdrObj := &unstructured.Unstructured{}
			mdrObj.SetGroupVersionKind(mdrGVK)

			getErr := APIClient.Get(ctx, types.NamespacedName{
				Name:      originalNodeName,
				Namespace: medik8sparams.OperatorNs,
			}, mdrObj)

			if getErr == nil {
				// MDR CR still exists -- remediation in progress.
				return false, nil
			}

			if !k8serrors.IsNotFound(getErr) {
				// Transient API error, retry.
				return false, nil
			}

			// MDR CR gone. Check if worker count has recovered.
			currentCount, countErr := helpers.CountReadyWorkerNodes(ctx, APIClient)
			if countErr != nil {
				return false, nil
			}

			if currentCount < expectedWorkerCount {
				return false, nil
			}

			// Worker count restored. Find the replacement node -- a worker
			// whose name was NOT in the initial set (new VM = new hostname).
			nodeList := &corev1.NodeList{}
			if listErr := APIClient.List(ctx, nodeList,
				client.MatchingLabels{"node-role.kubernetes.io/worker": ""}); listErr != nil {
				return false, nil
			}

			for i := range nodeList.Items {
				node := &nodeList.Items[i]
				if !initialWorkerNames[node.Name] {
					newNodeName = node.Name
					GinkgoWriter.Printf(
						"MDR remediation complete: replacement node %s (not in initial set, original: %s)\n",
						newNodeName, originalNodeName)

					return true, nil
				}
			}

			return false, nil
		},
	)

	return newNodeName, err
}

// getMDRCRCondition returns the named status condition from an unstructured MDR CR, or nil.
func getMDRCRCondition(nodeName, condType string) (map[string]interface{}, error) {
	mdrObj := &unstructured.Unstructured{}
	mdrObj.SetGroupVersionKind(mdrGVK)

	if err := APIClient.Get(context.TODO(), types.NamespacedName{
		Name:      nodeName,
		Namespace: medik8sparams.OperatorNs,
	}, mdrObj); err != nil {
		return nil, err
	}

	conditions, found, err := unstructured.NestedSlice(mdrObj.Object, "status", "conditions")
	if err != nil || !found {
		return nil, fmt.Errorf("no conditions found on MDR CR %s", nodeName)
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		if cond["type"] == condType {
			return cond, nil
		}
	}

	return nil, fmt.Errorf("condition %s not found on MDR CR %s", condType, nodeName)
}
