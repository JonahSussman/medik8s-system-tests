package helpers

import (
	"context"
	"fmt"
	"strings"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeleteRemediationCR deletes an unstructured remediation CR by GVK and name,
// polling until the resource is gone or the timeout expires.
func DeleteRemediationCR(
	ctx context.Context, k8sClient client.Client,
	gvk schema.GroupVersionKind, name, namespace string,
	pollInterval, timeout time.Duration,
	logf func(string, ...interface{}),
) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	key := client.ObjectKey{Name: name, Namespace: namespace}

	if waitErr := wait.PollUntilContextTimeout(
		ctx, pollInterval, timeout, true,
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
		logf("Warning: %s %s not fully deleted within %s: %v\n",
			gvk.Kind, name, timeout, waitErr)
	}
}

// GetActiveControllerNode returns the node name hosting the active controller
// pod by inspecting the leader election Lease.
func GetActiveControllerNode(
	ctx context.Context, k8sClient client.Client,
	leaseName, namespace string,
) (string, error) {
	lease := &coordinationv1.Lease{}

	if err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      leaseName,
		Namespace: namespace,
	}, lease); err != nil {
		if k8serrors.IsNotFound(err) {
			return "", fmt.Errorf("controller lease %q not found in namespace %s",
				leaseName, namespace)
		}

		return "", fmt.Errorf("failed to get controller lease: %w", err)
	}

	if lease.Spec.HolderIdentity == nil {
		return "", fmt.Errorf("controller lease %q has no holder", leaseName)
	}

	holderID := *lease.Spec.HolderIdentity

	podName, _, ok := strings.Cut(holderID, "_")
	if !ok || podName == "" {
		return "", fmt.Errorf("unexpected leader holderIdentity format: %q", holderID)
	}

	pod := &corev1.Pod{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
		return "", fmt.Errorf("failed to get leader pod %s: %w", podName, err)
	}

	if pod.Spec.NodeName == "" {
		return "", fmt.Errorf("leader pod %s is not scheduled to a node", podName)
	}

	return pod.Spec.NodeName, nil
}
