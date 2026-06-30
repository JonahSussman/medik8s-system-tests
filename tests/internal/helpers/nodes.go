package helpers

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsNodeReady returns true if the node has a Ready condition with status True.
func IsNodeReady(node *corev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			return cond.Status == corev1.ConditionTrue
		}
	}

	return false
}

// SelectWorkerNode returns a Ready, schedulable worker node that is not in the
// excludeNodes list. Returns an error if no eligible node is found.
func SelectWorkerNode(ctx context.Context, k8sClient client.Client, excludeNodes ...string) (*corev1.Node, error) {
	nodeList := &corev1.NodeList{}

	if err := k8sClient.List(ctx, nodeList, client.MatchingLabels{"node-role.kubernetes.io/worker": ""}); err != nil {
		return nil, fmt.Errorf("failed to list worker nodes: %w", err)
	}

	sort.Slice(nodeList.Items, func(i, j int) bool {
		return nodeList.Items[i].Name < nodeList.Items[j].Name
	})

	excluded := make(map[string]bool, len(excludeNodes))
	for _, name := range excludeNodes {
		excluded[name] = true
	}

	for i := range nodeList.Items {
		node := &nodeList.Items[i]

		if excluded[node.Name] || node.Spec.Unschedulable {
			continue
		}

		if IsNodeReady(node) {
			return node, nil
		}
	}

	return nil, fmt.Errorf("no eligible Ready worker node found (excluded: %v)", excludeNodes)
}

// CountReadyWorkerNodes returns the number of Ready, schedulable worker nodes.
func CountReadyWorkerNodes(ctx context.Context, k8sClient client.Client) (int, error) {
	nodeList := &corev1.NodeList{}
	if err := k8sClient.List(ctx, nodeList, client.MatchingLabels{"node-role.kubernetes.io/worker": ""}); err != nil {
		return 0, fmt.Errorf("failed to list worker nodes: %w", err)
	}

	count := 0

	for i := range nodeList.Items {
		node := &nodeList.Items[i]

		if node.Spec.Unschedulable {
			continue
		}

		if IsNodeReady(node) {
			count++
		}
	}

	return count, nil
}
