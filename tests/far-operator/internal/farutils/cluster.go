package farutils

import (
	"context"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
	"github.com/medik8s/system-tests/tests/internal/helpers"
)

// FenceAgentForPlatform returns the fence agent binary name and node identifier
// parameter for the given platform.
func FenceAgentForPlatform(platform configv1.PlatformType) (agent, nodeIDParam string, err error) {
	switch platform { //nolint:exhaustive // only AWS and BareMetal are supported; default rejects all others.
	case configv1.AWSPlatformType:
		return farparams.FenceAgentAWS, farparams.NodeIdentifierAWS, nil
	case configv1.BareMetalPlatformType:
		return farparams.FenceAgentIPMI, farparams.NodeIdentifierIPMI, nil
	default:
		return "", "", fmt.Errorf("unsupported platform for FAR destructive tests: %s", platform)
	}
}

// GetAWSCredentials reads the CCO-provisioned Secret and returns the access key
// and secret key for fence_aws.
func GetAWSCredentials(
	ctx context.Context, k8sClient client.Client, namespace string,
) (accessKey, secretKey string, err error) {
	secret := &corev1.Secret{}
	key := client.ObjectKey{
		Name:      farparams.AWSCredentialsSecretName,
		Namespace: namespace,
	}

	if err := k8sClient.Get(ctx, key, secret); err != nil {
		return "", "", fmt.Errorf("failed to get AWS credentials secret %s/%s: %w",
			namespace, farparams.AWSCredentialsSecretName, err)
	}

	accessKey = string(secret.Data[farparams.AWSAccessKeyField])
	secretKey = string(secret.Data[farparams.AWSSecretKeyField])

	if accessKey == "" || secretKey == "" {
		return "", "", fmt.Errorf("AWS credentials secret is missing required keys (%s, %s)",
			farparams.AWSAccessKeyField, farparams.AWSSecretKeyField)
	}

	return accessKey, secretKey, nil
}

// BuildAWSNodeParameters builds the --plug node parameter map for fence_aws
// from the list of worker nodes.
func BuildAWSNodeParameters(ctx context.Context, k8sClient client.Client) (map[string]map[string]string, error) {
	nodeList := &corev1.NodeList{}
	if err := k8sClient.List(ctx, nodeList, client.MatchingLabels{"node-role.kubernetes.io/worker": ""}); err != nil {
		return nil, fmt.Errorf("failed to list worker nodes: %w", err)
	}

	plugMap := make(map[string]string)

	for i := range nodeList.Items {
		node := &nodeList.Items[i]

		if node.Spec.Unschedulable || !helpers.IsNodeReady(node) {
			continue
		}

		instanceID, err := helpers.ExtractAWSInstanceID(node)
		if err != nil {
			return nil, fmt.Errorf("ready worker %s has invalid providerID: %w", node.Name, err)
		}

		plugMap[node.Name] = instanceID
	}

	if len(plugMap) == 0 {
		return nil, fmt.Errorf("no worker nodes with valid AWS providerID")
	}

	return map[string]map[string]string{farparams.NodeIdentifierAWS: plugMap}, nil
}
