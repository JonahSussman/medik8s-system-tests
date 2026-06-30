package farutils

import (
	"context"
	"fmt"
	"strings"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
)

// GetActiveFARControllerNode returns the node name hosting the active FAR
// controller pod by inspecting the leader election lease.
func GetActiveFARControllerNode(ctx context.Context, k8sClient client.Client) (string, error) {
	lease := &coordinationv1.Lease{}

	if err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      farparams.ControllerLeaseName,
		Namespace: medik8sparams.OperatorNs,
	}, lease); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("controller lease %q not found in namespace %s",
				farparams.ControllerLeaseName, medik8sparams.OperatorNs)
		}

		return "", fmt.Errorf("failed to get controller lease: %w", err)
	}

	if lease.Spec.HolderIdentity == nil {
		return "", fmt.Errorf("controller lease %q has no holder", farparams.ControllerLeaseName)
	}

	holderID := *lease.Spec.HolderIdentity

	podName, _, ok := strings.Cut(holderID, "_")
	if !ok || podName == "" {
		return "", fmt.Errorf("unexpected FAR leader holderIdentity format: %q", holderID)
	}

	pod := &corev1.Pod{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: medik8sparams.OperatorNs}, pod); err != nil {
		return "", fmt.Errorf("failed to get FAR leader pod %s: %w", podName, err)
	}

	if pod.Spec.NodeName == "" {
		return "", fmt.Errorf("FAR leader pod %s is not scheduled to a node", podName)
	}

	return pod.Spec.NodeName, nil
}

// GetFARControllerPods returns the running FAR controller manager pods.
func GetFARControllerPods(ctx context.Context, k8sClient client.Client) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}
	opts := []client.ListOption{
		client.InNamespace(medik8sparams.OperatorNs),
		client.MatchingLabels(farparams.OperatorControllerPodLabels),
	}

	if err := k8sClient.List(ctx, podList, opts...); err != nil {
		return nil, fmt.Errorf("failed to list FAR controller pods: %w", err)
	}

	var running []corev1.Pod

	for _, pod := range podList.Items {
		if pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil {
			continue
		}

		if len(pod.Status.ContainerStatuses) == 0 ||
			len(pod.Status.ContainerStatuses) != len(pod.Spec.Containers) {
			continue
		}

		allReady := true

		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				allReady = false

				break
			}
		}

		if allReady {
			running = append(running, pod)
		}
	}

	return running, nil
}
