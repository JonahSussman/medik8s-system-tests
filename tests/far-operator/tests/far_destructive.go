package tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
	"github.com/medik8s/system-tests/tests/far-operator/internal/farutils"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
)

var farGVK = schema.GroupVersionKind{
	Group:   "fence-agents-remediation.medik8s.io",
	Version: "v1alpha1",
	Kind:    "FenceAgentsRemediation",
}

var fartGVK = schema.GroupVersionKind{
	Group:   "fence-agents-remediation.medik8s.io",
	Version: "v1alpha1",
	Kind:    "FenceAgentsRemediationTemplate",
}

var _ = Describe("FAR Destructive Tests",
	Serial, Ordered, ContinueOnFailure,
	Label(labels.OperatorFAR, farparams.Label,
		labels.DisruptionDestructive,
		labels.PlatformAWS, labels.FrequencyWeekly),
	func() {
		var (
			ctx             context.Context
			platform        configv1.PlatformType
			region          string
			fenceAgent      string
			nodeIDParam     string
			awsAccessKey    string
			awsSecretKey    string
			leaderNode      string
			targetNode      *corev1.Node
			sharedParams    map[string]interface{}
			nodeParams      map[string]interface{}
			currentFARTName string
			currentFARName  string
		)

		BeforeAll(func() {
			ctx = context.Background()

			By("Detecting cluster platform")

			var err error

			platform, region, err = helpers.DetectPlatform(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())

			if platform != configv1.AWSPlatformType {
				Skip(fmt.Sprintf(
					"FAR destructive tests require AWS, got %s", platform))
			}

			By("Resolving fence agent for platform")

			fenceAgent, nodeIDParam, err = farutils.FenceAgentForPlatform(platform)
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf(
				"Platform: %s, Agent: %s, Region: %s\n",
				platform, fenceAgent, region)

			By("Verifying FAR operator deployment is ready")

			farDeployment, err := deployment.Pull(
				APIClient, farparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get FAR deployment")
			Expect(farDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"FAR deployment is not Ready")

			By("Verifying at least 3 Ready worker nodes")

			workerCount, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())
			// 3 workers: FAR leader (excluded from fencing) + target (fenced/rebooted) +
			// at least 1 spare to keep the cluster schedulable while the target is down.
			Expect(workerCount).To(
				BeNumerically(">=", 3),
				"Destructive tests require at least 3 Ready worker nodes")

			By("Reading AWS credentials from CCO Secret")

			awsAccessKey, awsSecretKey, err = farutils.GetAWSCredentials(
				ctx, APIClient, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(),
				"AWS credentials must be provisioned by the "+
					"medik8s-aws-credentials CI step")

			By("Building fence_aws shared parameters")

			sharedParams = map[string]interface{}{
				"--region":          region,
				"--action":          "reboot",
				"--skip-race-check": "",
				"--access-key":      awsAccessKey,
				"--secret-key":      awsSecretKey,
			}

			By("Building node parameters (--plug = EC2 instance ID)")

			awsNodeParams, err := farutils.BuildAWSNodeParameters(
				ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())

			nodeParams = make(map[string]interface{})

			for paramName, nodeMap := range awsNodeParams {
				inner := make(map[string]interface{}, len(nodeMap))
				for nodeName, val := range nodeMap {
					inner[nodeName] = val
				}

				nodeParams[paramName] = inner
			}

			By("Identifying active FAR controller node")

			leaderNode, err = farutils.GetActiveFARControllerNode(
				ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf("FAR leader is on node: %s\n", leaderNode)

			// TODO(RHWA-963): remove when destructive test specs consume these variables.
			_ = nodeIDParam
			_ = sharedParams
			_ = nodeParams
		})

		JustAfterEach(func() {
			spec := CurrentSpecReport()
			if spec.Failed() {
				GinkgoWriter.Println(
					"Test failed - running safety net cleanup")
			}

			if currentFARName != "" {
				By("Safety net: deleting FAR CR " + currentFARName)
				deleteRemediationCR(ctx, APIClient, farGVK, currentFARName)
				currentFARName = ""
			}

			if currentFARTName != "" {
				By("Safety net: deleting FART " + currentFARTName)
				deleteRemediationCR(ctx, APIClient, fartGVK, currentFARTName)
				currentFARTName = ""
			}

			if targetNode != nil {
				nodeName := targetNode.Name
				targetNode = nil

				By("Safety net: ensuring kubelet is running on " + nodeName)
				Expect(farutils.StartKubelet(ctx, nodeName)).To(Succeed(),
					"safety net: failed to restart kubelet on %s", nodeName)

				By("Safety net: waiting for node to become Ready")
				Expect(farutils.WaitForNodeReady(
					ctx, APIClient, nodeName,
					farparams.NodeReadyTimeout)).To(Succeed(),
					"safety net: node %s did not become Ready", nodeName)
			}
		})

		Context("Standalone FAR remediation", func() {
			// RHWA-963: 7 standalone destructive tests will be added here.
			// Each test follows this flow:
			//   1. Select target worker (exclude leader node)
			//   2. Record boot ID
			//   3. Create FART + deploy workload pod
			//   4. Stop kubelet (simulate unhealthy node)
			//   5. WaitForNodeNotReady (verify kubelet actually stopped)
			//   6. Create FAR CR (trigger remediation)
			//   7. Verify: taint applied, node rebooted, pod evicted
			//   8. Cleanup via DeferCleanup + JustAfterEach safety net
		})

		Context("NHC+FAR interop", func() {
			// RHWA-1035: 4 NHC+FAR interop tests will be added here.
			// These tests install both NHC and FAR, configure NHC to use
			// FAR as the remediator, then trigger remediation via NHC by
			// stopping kubelet and waiting for NHC to detect the unhealthy
			// node and create a FAR CR automatically.
		})
	})

//nolint:unused // scaffold helper for upcoming destructive test specs
func buildFARTUnstructured(
	name, agent string,
	sharedParams, nodeParams map[string]interface{},
) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "fence-agents-remediation.medik8s.io/v1alpha1",
			"kind":       "FenceAgentsRemediationTemplate",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": medik8sparams.OperatorNs,
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"agent":               agent,
						"sharedparameters":    sharedParams,
						"nodeparameters":      nodeParams,
						"retrycount":          10,
						"retryinterval":       "20s",
						"timeout":             "60s",
						"remediationStrategy": "OutOfServiceTaint",
					},
				},
			},
		},
	}
}

func deleteRemediationCR(
	ctx context.Context, k8sClient client.Client,
	gvk schema.GroupVersionKind, name string,
) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	key := client.ObjectKey{Name: name, Namespace: medik8sparams.OperatorNs}

	if waitErr := wait.PollUntilContextTimeout(
		ctx, farparams.DefaultPollInterval, farparams.RemediationCRDeletionTimeout, true,
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
			gvk.Kind, name, farparams.RemediationCRDeletionTimeout, waitErr)
	}
}
