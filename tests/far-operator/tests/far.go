package tests

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	olmV1alpha1 "github.com/rh-ecosystem-edge/eco-goinfra/pkg/schemes/olm/operators/v1alpha1"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/infrastructure"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe(
	"FAR Post Deployment tests",
	Ordered,
	ContinueOnFailure,
	Label(farparams.Label), func() {
		var farDeployment *deployment.Builder

		BeforeAll(func() {
			By("Get FAR deployment object")

			var err error

			farDeployment, err = deployment.Pull(
				APIClient, farparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get FAR deployment")

			By("Verify FAR deployment is Ready")
			Expect(farDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(), "FAR deployment is not Ready")
		})
		It("Verify Fence Agents Remediation Operator pod is running", reportxml.ID("66026"), func() {
			listOptions := metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", farparams.OperatorControllerPodLabel),
			}
			_, err := pod.WaitForAllPodsInNamespaceRunning(
				APIClient,
				medik8sparams.OperatorNs,
				medik8sparams.DefaultTimeout,
				listOptions,
			)
			Expect(err).ToNot(HaveOccurred(), "Pod is not ready")

			By("Verifying pod count matches expected replicas")

			farPods, err := pod.List(APIClient, medik8sparams.OperatorNs, listOptions)
			Expect(err).ToNot(HaveOccurred(), "Failed to list FAR pods")

			runningPods := filterRunningPods(farPods)

			infraConfig, err := infrastructure.Pull(APIClient)
			Expect(err).ToNot(HaveOccurred(), "Failed to pull infrastructure configuration")

			expectedCount := farparams.ExpectedReplicas
			if infraConfig.Object.Status.ControlPlaneTopology == configv1.SingleReplicaTopologyMode {
				expectedCount = int32(1)
			}

			Expect(int32(len(runningPods))).To(Equal(expectedCount),
				"Expected %d running FAR pod(s), found %d", expectedCount, len(runningPods))
		})

		It("Verify FAR CSV has required annotations", reportxml.ID("70637"), func() {
			By("Getting FAR ClusterServiceVersion")

			var farCSV *olm.ClusterServiceVersionBuilder

			Eventually(func() error {
				farCSVs, err := olm.ListClusterServiceVersionWithNamePattern(
					APIClient, "fence-agents-remediation", medik8sparams.OperatorNs)
				if err != nil {
					return err
				}

				for _, csv := range farCSVs {
					phase, phaseErr := csv.GetPhase()
					if phaseErr == nil && phase == olmV1alpha1.CSVPhaseSucceeded {
						farCSV = csv

						return nil
					}
				}

				return fmt.Errorf("no FAR CSV in Succeeded phase found yet")
			}, medik8sparams.DefaultTimeout, farparams.DefaultPollInterval).Should(Succeed(),
				"FAR CSV must reach Succeeded phase")

			By("Checking annotation values on FAR CSV")

			Expect(farCSV.Object.Annotations).ToNot(BeNil(), "CSV annotations should not be nil")

			for annotationKey, expectedValue := range farparams.RequiredAnnotations {
				annotationValue, exists := farCSV.Object.Annotations[annotationKey]
				Expect(exists).To(BeTrue(), "Required annotation %q should exist on FAR CSV", annotationKey)
				Expect(annotationValue).To(Equal(expectedValue), "Annotation %q should have value %q", annotationKey, expectedValue)
			}
		})

		It("Verify FAR controller manager has correct number of replicas", reportxml.ID("61222"), func() {
			By("Checking cluster topology")

			infraConfig, err := infrastructure.Pull(APIClient)
			Expect(err).ToNot(HaveOccurred(), "Failed to pull infrastructure configuration")

			if infraConfig.Object.Status.ControlPlaneTopology == configv1.SingleReplicaTopologyMode {
				Skip("Skipping test on SNO (Single Node OpenShift) cluster")
			}

			By("Checking deployment replicas")
			Expect(farDeployment.Object.Spec.Replicas).ToNot(BeNil(), "Deployment replicas should not be nil")
			Expect(*farDeployment.Object.Spec.Replicas).To(Equal(farparams.ExpectedReplicas),
				"Expected %d replica(s), found %d", farparams.ExpectedReplicas, *farDeployment.Object.Spec.Replicas)

			By("Verifying ready replicas")
			Expect(farDeployment.Object.Status.ReadyReplicas).To(Equal(farparams.ExpectedReplicas),
				"Expected %d ready replica(s), found %d", farparams.ExpectedReplicas, farDeployment.Object.Status.ReadyReplicas)

			By("Verifying pods run on different nodes")

			listOptions := metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", farparams.OperatorControllerPodLabel),
			}

			farPods, err := pod.List(APIClient, medik8sparams.OperatorNs, listOptions)
			Expect(err).ToNot(HaveOccurred(), "Failed to list FAR pods")

			runningPods := filterRunningPods(farPods)

			nodeNames := make(map[string]bool)

			for _, p := range runningPods {
				Expect(p.Object.Spec.NodeName).ToNot(BeEmpty(),
					"Pod %s has not been assigned to a node", p.Object.Name)
				nodeNames[p.Object.Spec.NodeName] = true
			}

			Expect(len(nodeNames)).To(Equal(int(farparams.ExpectedReplicas)),
				"FAR pods must run on different nodes for HA, but found pods on %d unique node(s)", len(nodeNames))
		})

		It("Verify FAR container runs as non-root user", reportxml.ID("89231"), func() {
			By("Getting FAR controller pod names")

			listOptions := metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", farparams.OperatorControllerPodLabel),
			}
			farPods, err := pod.List(APIClient, medik8sparams.OperatorNs, listOptions)
			Expect(err).ToNot(HaveOccurred(), "Failed to get FAR controller pods")

			runningPods := filterRunningPods(farPods)
			Expect(len(runningPods)).To(BeNumerically(">", 0), "No running FAR controller pods found")

			var errorMessages []string

			for _, farPod := range runningPods {
				By(fmt.Sprintf("Verifying security context for pod %s", farPod.Object.Name))

				By("Checking pod-level runAsNonRoot security context")

				if farPod.Object.Spec.SecurityContext == nil {
					errorMessages = append(errorMessages,
						fmt.Sprintf("Pod %s has nil SecurityContext", farPod.Object.Name))
				} else if farPod.Object.Spec.SecurityContext.RunAsNonRoot == nil {
					errorMessages = append(errorMessages,
						fmt.Sprintf("Pod %s has nil runAsNonRoot", farPod.Object.Name))
				} else if !*farPod.Object.Spec.SecurityContext.RunAsNonRoot {
					errorMessages = append(errorMessages,
						fmt.Sprintf("Incorrect runAsNonRoot for pod %s. Expected true, found: %v",
							farPod.Object.Name, *farPod.Object.Spec.SecurityContext.RunAsNonRoot))
				}

				By("Checking manager container security context")

				managerFound := false

				for _, container := range farPod.Object.Spec.Containers {
					if container.Name != farparams.ManagerContainerName {
						continue
					}

					managerFound = true
					securityContext := container.SecurityContext

					if securityContext == nil {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Container %s in pod %s has nil SecurityContext",
								container.Name, farPod.Object.Name))

						continue
					}

					if securityContext.RunAsUser != nil && *securityContext.RunAsUser == 0 {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Container %s in pod %s runs as root (UID 0)",
								container.Name, farPod.Object.Name))
					}

					if securityContext.AllowPrivilegeEscalation == nil || *securityContext.AllowPrivilegeEscalation {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Container %s in pod %s: AllowPrivilegeEscalation must be explicitly false",
								container.Name, farPod.Object.Name))
					}

					if securityContext.Capabilities == nil {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Container %s in pod %s: Capabilities block is nil, must drop ALL",
								container.Name, farPod.Object.Name))
					} else {
						hasDropAll := false

						for _, cap := range securityContext.Capabilities.Drop {
							if cap == "ALL" {
								hasDropAll = true

								break
							}
						}

						if !hasDropAll {
							errorMessages = append(errorMessages,
								fmt.Sprintf("Container %s in pod %s does not drop ALL capabilities",
									container.Name, farPod.Object.Name))
						}
					}

					if securityContext.ReadOnlyRootFilesystem == nil || !*securityContext.ReadOnlyRootFilesystem {
						errorMessages = append(errorMessages,
							fmt.Sprintf(
								"Container %s in pod %s: ReadOnlyRootFilesystem must be explicitly true",
								container.Name, farPod.Object.Name))
					}

					seccompOk := false
					if securityContext.SeccompProfile != nil &&
						securityContext.SeccompProfile.Type == corev1.SeccompProfileTypeRuntimeDefault {
						seccompOk = true
					} else if farPod.Object.Spec.SecurityContext != nil &&
						farPod.Object.Spec.SecurityContext.SeccompProfile != nil &&
						farPod.Object.Spec.SecurityContext.SeccompProfile.Type == corev1.SeccompProfileTypeRuntimeDefault {
						seccompOk = true
					}

					if !seccompOk {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Container %s in pod %s missing RuntimeDefault seccomp profile",
								container.Name, farPod.Object.Name))
					}
				}

				if !managerFound {
					errorMessages = append(errorMessages,
						fmt.Sprintf("Pod %s has no container named %q",
							farPod.Object.Name, farparams.ManagerContainerName))
				}
			}

			if len(errorMessages) > 0 {
				errMsg := "Testing security context of FAR container failed due to:\n"
				for _, msg := range errorMessages {
					errMsg += fmt.Sprintf("- %s\n", msg)
				}

				Fail(errMsg)
			}
		})
	})

func filterRunningPods(pods []*pod.Builder) []*pod.Builder {
	running := make([]*pod.Builder, 0, len(pods))

	for _, podBuilder := range pods {
		if podBuilder.Object.Status.Phase != corev1.PodRunning || podBuilder.Object.DeletionTimestamp != nil {
			continue
		}

		allReady := true

		for _, cs := range podBuilder.Object.Status.ContainerStatuses {
			if !cs.Ready {
				allReady = false

				break
			}
		}

		if allReady {
			running = append(running, podBuilder)
		}
	}

	return running
}
