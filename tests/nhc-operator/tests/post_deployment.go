package tests

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/infrastructure"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/nhc-operator/internal/nhcparams"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe(
	"NHC Post Deployment tests",
	Ordered,
	ContinueOnFailure,
	Label(nhcparams.Label), func() {
		var (
			nhcCSV               *olm.ClusterServiceVersionBuilder
			controlPlaneTopology configv1.TopologyMode
		)

		BeforeAll(func() {
			By("Get NHC deployment object and verify readiness")

			nhcDeployment, err := deployment.Pull(
				APIClient, nhcparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get NHC deployment")
			Expect(nhcDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(), "NHC deployment is not Ready")

			By("Pull cluster topology for use in topology-aware tests")

			infraConfig, infraErr := infrastructure.Pull(APIClient)
			Expect(infraErr).ToNot(HaveOccurred(), "Failed to pull infrastructure configuration")

			controlPlaneTopology = infraConfig.Object.Status.ControlPlaneTopology

			By("Get NHC ClusterServiceVersion")

			Eventually(func() error {
				nhcCSVs, listErr := olm.ListClusterServiceVersionWithNamePattern(
					APIClient, nhcparams.CSVNamePattern, medik8sparams.OperatorNs)
				if listErr != nil {
					return fmt.Errorf("failed to list NHC ClusterServiceVersions: %w", listErr)
				}

				if len(nhcCSVs) == 0 {
					return fmt.Errorf("no NHC ClusterServiceVersion found in namespace %s",
						medik8sparams.OperatorNs)
				}

				var (
					newestCSV  *olm.ClusterServiceVersionBuilder
					newestTime metav1.Time
				)

				for _, csv := range nhcCSVs {
					phase, phaseErr := csv.GetPhase()
					if phaseErr == nil && phase == "Succeeded" {
						if newestCSV == nil || csv.Object.CreationTimestamp.After(newestTime.Time) {
							newestCSV = csv
							newestTime = csv.Object.CreationTimestamp
						}
					}
				}

				if newestCSV != nil {
					nhcCSV = newestCSV

					return nil
				}

				return fmt.Errorf("no NHC CSV in Succeeded phase found yet")
			}, medik8sparams.DefaultTimeout, nhcparams.DefaultPollInterval).Should(Succeed(),
				"NHC CSV must reach Succeeded phase")
		})

		It("Verify NHC resources are installed and running",
			reportxml.ID("89629"),
			Label(labels.OperatorNHC, labels.TierSmoke, labels.DisruptionNonDestructive,
				labels.PlatformAny, labels.FrequencyPresubmit,
				labels.ComponentController), func() {
				By("Verifying NodeHealthCheck API is available")

				nhcList := &unstructured.UnstructuredList{}
				nhcList.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   nhcparams.CRDGroup,
					Version: nhcparams.CRDVersion,
					Kind:    "NodeHealthCheckList",
				})

				err := APIClient.List(context.TODO(), nhcList)
				Expect(err).ToNot(HaveOccurred(),
					"NodeHealthCheck CRD should be installed and listable")

				By("Verifying NHC controller-manager pods are running")

				ctrlListOptions := metav1.ListOptions{
					LabelSelector: nhcparams.OperatorControllerPodLabelSelector,
				}

				_, err = pod.WaitForAllPodsInNamespaceRunning(
					APIClient,
					medik8sparams.OperatorNs,
					medik8sparams.DefaultTimeout,
					ctrlListOptions,
				)
				Expect(err).ToNot(HaveOccurred(), "NHC controller pods are not running")
			})

		It("Verify NHC CSV annotations",
			reportxml.ID("89630"),
			Label(labels.OperatorNHC, labels.TierSmoke, labels.DisruptionNonDestructive,
				labels.PlatformAny, labels.FrequencyPresubmit,
				labels.ComponentOLM), func() {
				Expect(nhcCSV).ToNot(BeNil(),
					"NHC CSV was not resolved in BeforeAll - is the operator installed?")

				By("Checking valid-subscription annotation")

				annotations := nhcCSV.Object.Annotations
				Expect(annotations).ToNot(BeNil(), "CSV annotations should not be nil")

				_, hasValidSubscription := annotations["operators.openshift.io/valid-subscription"]
				Expect(hasValidSubscription).To(BeTrue(),
					"CSV should have operators.openshift.io/valid-subscription annotation")

				By("Checking support annotation")

				supportValue, hasSupport := annotations["support"]
				Expect(hasSupport).To(BeTrue(), "CSV should have support annotation")
				Expect(strings.TrimSpace(supportValue)).ToNot(BeEmpty(),
					"CSV support annotation should not be empty")

				By("Checking repository annotation")

				repoValue, hasRepository := annotations["repository"]
				Expect(hasRepository).To(BeTrue(), "CSV should have repository annotation")
				Expect(strings.TrimSpace(repoValue)).ToNot(BeEmpty(),
					"CSV repository annotation should not be empty")

				By("Checking maintainers")

				maintainers := nhcCSV.Object.Spec.Maintainers
				Expect(len(maintainers)).To(BeNumerically(">", 0),
					"CSV should have at least one maintainer")
			})

		It("Verify NHC CSV metadata",
			reportxml.ID("89631"),
			Label(labels.OperatorNHC, labels.TierSmoke, labels.DisruptionNonDestructive,
				labels.PlatformAny, labels.FrequencyPresubmit,
				labels.ComponentOLM), func() {
				Expect(nhcCSV).ToNot(BeNil(),
					"NHC CSV was not resolved in BeforeAll - is the operator installed?")

				By("Checking required CSV annotations")

				annotations := nhcCSV.Object.Annotations
				Expect(annotations).ToNot(BeNil(), "CSV annotations should not be nil")

				var annotationErrors []string

				for annotationKey, expectedValue := range nhcparams.RequiredAnnotations {
					annotationValue, exists := annotations[annotationKey]
					if !exists {
						annotationErrors = append(annotationErrors,
							fmt.Sprintf("required annotation %q is missing", annotationKey))

						continue
					}

					if annotationValue != expectedValue {
						annotationErrors = append(annotationErrors,
							fmt.Sprintf("annotation %q: expected %q, got %q",
								annotationKey, expectedValue, annotationValue))
					}
				}

				if len(annotationErrors) > 0 {
					errMsg := "NHC CSV annotation validation failures:\n"
					for _, msg := range annotationErrors {
						errMsg += fmt.Sprintf("- %s\n", msg)
					}

					Fail(errMsg)
				}

				By("Checking replaces field when present")

				replaces := nhcCSV.Object.Spec.Replaces
				if replaces != "" {
					Expect(replaces).To(ContainSubstring(nhcparams.CSVNamePattern),
						"replaces field should contain %q, got %q", nhcparams.CSVNamePattern, replaces)
				} else {
					GinkgoWriter.Printf("CSV %q has empty spec.replaces (expected on first install)\n",
						nhcCSV.Object.Name)
				}

				By("Checking cluster topology for replica validation")

				if controlPlaneTopology == configv1.SingleReplicaTopologyMode {
					Skip("Skipping replica validation on SNO (Single Node OpenShift) cluster")
				}

				By("Verifying controller replicas on multi-node cluster")

				Eventually(func() error {
					liveDeploy, pullErr := deployment.Pull(
						APIClient, nhcparams.OperatorDeploymentName, medik8sparams.OperatorNs)
					if pullErr != nil {
						return fmt.Errorf("failed to pull deployment: %w", pullErr)
					}

					if liveDeploy.Object.Spec.Replicas == nil {
						return fmt.Errorf("deployment replicas is nil")
					}

					if *liveDeploy.Object.Spec.Replicas != nhcparams.ExpectedReplicas {
						return fmt.Errorf("expected %d replica(s), found %d",
							nhcparams.ExpectedReplicas, *liveDeploy.Object.Spec.Replicas)
					}

					if liveDeploy.Object.Status.ReadyReplicas != nhcparams.ExpectedReplicas {
						return fmt.Errorf("expected %d ready replica(s), found %d",
							nhcparams.ExpectedReplicas, liveDeploy.Object.Status.ReadyReplicas)
					}

					return nil
				}, medik8sparams.DefaultTimeout, nhcparams.DefaultPollInterval).Should(Succeed(),
					"NHC controller replicas not matching expected count")
			})

		It("Verify NHC container runs as non-root user",
			reportxml.ID("89632"),
			Label(labels.OperatorNHC, labels.TierSmoke, labels.DisruptionNonDestructive,
				labels.PlatformAny, labels.FrequencyPresubmit,
				labels.ComponentController), func() {
				By("Waiting for NHC controller pods to be running")

				listOptions := metav1.ListOptions{
					LabelSelector: nhcparams.OperatorControllerPodLabelSelector,
				}

				_, err := pod.WaitForAllPodsInNamespaceRunning(
					APIClient,
					medik8sparams.OperatorNs,
					medik8sparams.DefaultTimeout,
					listOptions,
				)
				Expect(err).ToNot(HaveOccurred(), "NHC controller pods are not ready")

				By("Listing NHC controller pods")

				nhcPods, err := pod.List(APIClient, medik8sparams.OperatorNs, listOptions)
				Expect(err).ToNot(HaveOccurred(), "Failed to list NHC controller pods")

				runningPods := filterRunningPods(nhcPods)
				Expect(runningPods).ToNot(BeEmpty(), "No running NHC controller pods found")

				var errorMessages []string

				for _, nhcPod := range runningPods {
					By(fmt.Sprintf("Verifying security context for pod %s", nhcPod.Object.Name))

					By("Checking pod-level runAsNonRoot security context")

					if nhcPod.Object.Spec.SecurityContext == nil {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Pod %s has nil SecurityContext", nhcPod.Object.Name))
					} else if nhcPod.Object.Spec.SecurityContext.RunAsNonRoot == nil {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Pod %s has nil runAsNonRoot", nhcPod.Object.Name))
					} else if !*nhcPod.Object.Spec.SecurityContext.RunAsNonRoot {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Incorrect runAsNonRoot for pod %s. Expected true, found: %v",
								nhcPod.Object.Name,
								*nhcPod.Object.Spec.SecurityContext.RunAsNonRoot))
					}

					By("Checking manager container security context")

					managerFound := false

					for _, container := range nhcPod.Object.Spec.Containers {
						if container.Name != nhcparams.ManagerContainerName {
							continue
						}

						managerFound = true
						securityContext := container.SecurityContext

						if securityContext == nil {
							errorMessages = append(errorMessages,
								fmt.Sprintf("Container %s in pod %s has nil SecurityContext",
									container.Name, nhcPod.Object.Name))

							continue
						}

						if securityContext.RunAsUser != nil && *securityContext.RunAsUser == 0 {
							errorMessages = append(errorMessages,
								fmt.Sprintf("Container %s in pod %s runs as root (UID 0)",
									container.Name, nhcPod.Object.Name))
						}

						if securityContext.AllowPrivilegeEscalation == nil || *securityContext.AllowPrivilegeEscalation {
							errorMessages = append(errorMessages,
								fmt.Sprintf(
									"Container %s in pod %s: AllowPrivilegeEscalation must be explicitly false",
									container.Name, nhcPod.Object.Name))
						}

						if securityContext.Capabilities == nil {
							errorMessages = append(errorMessages,
								fmt.Sprintf(
									"Container %s in pod %s: Capabilities block is nil, must drop ALL",
									container.Name, nhcPod.Object.Name))
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
										container.Name, nhcPod.Object.Name))
							}
						}

						if securityContext.ReadOnlyRootFilesystem == nil || !*securityContext.ReadOnlyRootFilesystem {
							errorMessages = append(errorMessages,
								fmt.Sprintf(
									"Container %s in pod %s: ReadOnlyRootFilesystem must be explicitly true",
									container.Name, nhcPod.Object.Name))
						}

						seccompOk := false
						if securityContext.SeccompProfile != nil &&
							securityContext.SeccompProfile.Type == corev1.SeccompProfileTypeRuntimeDefault {
							seccompOk = true
						} else if nhcPod.Object.Spec.SecurityContext != nil &&
							nhcPod.Object.Spec.SecurityContext.SeccompProfile != nil &&
							nhcPod.Object.Spec.SecurityContext.SeccompProfile.Type ==
								corev1.SeccompProfileTypeRuntimeDefault {
							seccompOk = true
						}

						if !seccompOk {
							errorMessages = append(errorMessages,
								fmt.Sprintf(
									"Container %s in pod %s missing RuntimeDefault seccomp profile",
									container.Name, nhcPod.Object.Name))
						}
					}

					if !managerFound {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Pod %s has no container named %q",
								nhcPod.Object.Name, nhcparams.ManagerContainerName))
					}
				}

				if len(errorMessages) > 0 {
					errMsg := "Testing security context of NHC container failed due to:\n"
					for _, msg := range errorMessages {
						errMsg += fmt.Sprintf("- %s\n", msg)
					}

					Fail(errMsg)
				}
			})
	})

func filterRunningPods(pods []*pod.Builder) []*pod.Builder {
	var running []*pod.Builder

	for _, candidate := range pods {
		if candidate.Object.Status.Phase != corev1.PodRunning || candidate.Object.DeletionTimestamp != nil {
			continue
		}

		allReady := true

		for _, cs := range candidate.Object.Status.ContainerStatuses {
			if !cs.Ready {
				allReady = false

				break
			}
		}

		if allReady {
			running = append(running, candidate)
		}
	}

	return running
}
