package tests

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	oplmV1alpha1 "github.com/rh-ecosystem-edge/eco-goinfra/pkg/schemes/olm/operators/v1alpha1"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/infrastructure"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/nmo-operator/internal/nmoparams"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func findActiveCSV(csvs []*olm.ClusterServiceVersionBuilder) *olm.ClusterServiceVersionBuilder {
	for _, csv := range csvs {
		phase, err := csv.GetPhase()
		if err == nil && phase == oplmV1alpha1.CSVPhaseSucceeded {
			return csv
		}
	}

	return nil
}

func filterRunningPods(pods []*pod.Builder) []*pod.Builder {
	var running []*pod.Builder

	for _, nmoPod := range pods {
		if nmoPod.Object.Status.Phase != corev1.PodRunning || nmoPod.Object.DeletionTimestamp != nil {
			continue
		}

		allReady := true

		for _, cs := range nmoPod.Object.Status.ContainerStatuses {
			if !cs.Ready {
				allReady = false

				break
			}
		}

		if allReady {
			running = append(running, nmoPod)
		}
	}

	return running
}

func fetchActiveCSV() *olm.ClusterServiceVersionBuilder {
	var nmoCSV *olm.ClusterServiceVersionBuilder

	Eventually(func() error {
		nmoCSVs, err := olm.ListClusterServiceVersionWithNamePattern(
			APIClient, nmoparams.CSVNamePattern, medik8sparams.OperatorNs)
		if err != nil {
			return fmt.Errorf("failed to list NMO ClusterServiceVersions: %w", err)
		}

		if len(nmoCSVs) == 0 {
			return fmt.Errorf("no NMO ClusterServiceVersion found in namespace %s", medik8sparams.OperatorNs)
		}

		nmoCSV = findActiveCSV(nmoCSVs)
		if nmoCSV == nil {
			return fmt.Errorf("no NMO CSV in Succeeded phase found yet")
		}

		return nil
	}, medik8sparams.DefaultTimeout, nmoparams.DefaultPollInterval).Should(Succeed(),
		"NMO CSV must reach Succeeded phase")

	return nmoCSV
}

var _ = Describe(
	"NMO Post Deployment tests",
	Ordered,
	ContinueOnFailure,
	Label(nmoparams.Label), func() {
		var controlPlaneTopology configv1.TopologyMode

		BeforeAll(func() {
			By("Get NMO deployment object and verify it is Ready")

			nmoDeployment, err := deployment.Pull(
				APIClient, nmoparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get NMO deployment")
			Expect(nmoDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"NMO deployment is not Ready")

			By("Pull cluster topology for consistency with other operator suites")

			infraConfig, infraErr := infrastructure.Pull(APIClient)
			Expect(infraErr).ToNot(HaveOccurred(), "Failed to pull infrastructure configuration")

			controlPlaneTopology = infraConfig.Object.Status.ControlPlaneTopology
			_ = controlPlaneTopology
		})

		It("Verify Node Maintenance Operator pod is running",
			reportxml.ID("46315"),
			Label(
				labels.DisruptionNonDestructive,
				labels.TierSmoke,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyPresubmit,
			), func() {
				listOptions := metav1.ListOptions{LabelSelector: nmoparams.OperatorControllerPodLabelSelector}

				By("Verifying pod count matches expected replicas")

				Eventually(func() error {
					nmoPods, listErr := pod.List(APIClient, medik8sparams.OperatorNs, listOptions)
					if listErr != nil {
						return listErr
					}

					for _, nmoPod := range nmoPods {
						if nmoPod.Object.DeletionTimestamp != nil {
							continue
						}

						if nmoPod.Object.Status.Phase != corev1.PodRunning {
							return fmt.Errorf("pod %s is in phase %s, expected Running",
								nmoPod.Object.Name, nmoPod.Object.Status.Phase)
						}
					}

					runningCount := int32(len(filterRunningPods(nmoPods)))

					if runningCount < nmoparams.ExpectedReplicas {
						return fmt.Errorf("expected at least %d running NMO pod(s), found %d",
							nmoparams.ExpectedReplicas, runningCount)
					}

					return nil
				}, medik8sparams.DefaultTimeout, nmoparams.DefaultPollInterval).Should(Succeed(),
					"NMO pods did not reach expected running count of %d", nmoparams.ExpectedReplicas)
			})

		It("Verify NMO CSV has required annotations",
			// TODO(RHWA-1146): assign Polarion ID when test case is created
			Label(
				labels.DisruptionNonDestructive,
				labels.TierSmoke,
				labels.PlatformAny,
				labels.ComponentOLM,
				labels.FrequencyPresubmit,
			), func() {
				By("Finding the active (Succeeded) CSV")

				nmoCSV := fetchActiveCSV()

				By("Checking annotation values on NMO CSV")

				Expect(nmoCSV.Object.Annotations).ToNot(BeNil(), "CSV annotations should not be nil")

				var annotationErrors []string

				for annotationKey, expectedValue := range nmoparams.RequiredAnnotations {
					annotationValue, exists := nmoCSV.Object.Annotations[annotationKey]
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
					errMsg := "NMO CSV annotation validation failures:\n"
					for _, msg := range annotationErrors {
						errMsg += fmt.Sprintf("- %s\n", msg)
					}

					Fail(errMsg)
				}
			})

		It("Verify NMO controller manager has correct number of replicas",
			// TODO(RHWA-1146): assign Polarion ID when test case is created
			Label(
				labels.DisruptionNonDestructive,
				labels.TierSmoke,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyPresubmit,
			), func() {
				By("Verifying replica count and ready replicas")

				Eventually(func() error {
					liveDeploy, pullErr := deployment.Pull(
						APIClient, nmoparams.OperatorDeploymentName, medik8sparams.OperatorNs)
					if pullErr != nil {
						return pullErr
					}

					if liveDeploy.Object.Spec.Replicas == nil ||
						*liveDeploy.Object.Spec.Replicas != nmoparams.ExpectedReplicas {
						desired := int32(0)
						if liveDeploy.Object.Spec.Replicas != nil {
							desired = *liveDeploy.Object.Spec.Replicas
						}

						return fmt.Errorf("expected %d desired replica(s), found %d",
							nmoparams.ExpectedReplicas, desired)
					}

					if liveDeploy.Object.Status.ReadyReplicas != nmoparams.ExpectedReplicas {
						return fmt.Errorf("expected %d ready replica(s), found %d",
							nmoparams.ExpectedReplicas, liveDeploy.Object.Status.ReadyReplicas)
					}

					return nil
				}, medik8sparams.DefaultTimeout, nmoparams.DefaultPollInterval).Should(Succeed(),
					"NMO deployment did not stabilise at %d ready replica(s)",
					nmoparams.ExpectedReplicas)
			})

		It("Verify NMO container runs as non-root user",
			// TODO(RHWA-1146): assign Polarion ID when test case is created
			Label(
				labels.DisruptionNonDestructive,
				labels.TierSmoke,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyPresubmit,
			), func() {
				By("Getting NMO controller pod names")

				listOptions := metav1.ListOptions{LabelSelector: nmoparams.OperatorControllerPodLabelSelector}

				var runningPods []*pod.Builder

				Eventually(func() error {
					nmoPods, listErr := pod.List(APIClient, medik8sparams.OperatorNs, listOptions)
					if listErr != nil {
						return listErr
					}

					running := filterRunningPods(nmoPods)
					if len(running) == 0 {
						return fmt.Errorf("no running NMO controller pods found")
					}

					runningPods = running

					return nil
				}, medik8sparams.DefaultTimeout, nmoparams.DefaultPollInterval).Should(Succeed(),
					"At least one running NMO controller pod should be found")

				var errorMessages []string

				for _, nmoPod := range runningPods {
					By(fmt.Sprintf("Verifying security context for pod %s", nmoPod.Object.Name))

					By("Checking pod-level runAsNonRoot security context")

					if nmoPod.Object.Spec.SecurityContext == nil {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Pod %s has nil SecurityContext", nmoPod.Object.Name))
					} else if nmoPod.Object.Spec.SecurityContext.RunAsNonRoot == nil {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Pod %s has nil runAsNonRoot", nmoPod.Object.Name))
					} else if !*nmoPod.Object.Spec.SecurityContext.RunAsNonRoot {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Incorrect runAsNonRoot for pod %s. Expected true, found: %v",
								nmoPod.Object.Name,
								*nmoPod.Object.Spec.SecurityContext.RunAsNonRoot))
					}

					By("Checking manager container security context")

					managerFound := false

					for _, container := range nmoPod.Object.Spec.Containers {
						if container.Name != nmoparams.ManagerContainerName {
							continue
						}

						managerFound = true
						securityContext := container.SecurityContext

						if securityContext == nil {
							errorMessages = append(errorMessages,
								fmt.Sprintf("Container %s in pod %s has nil SecurityContext",
									container.Name, nmoPod.Object.Name))

							continue
						}

						if securityContext.RunAsUser != nil && *securityContext.RunAsUser == 0 {
							errorMessages = append(errorMessages,
								fmt.Sprintf("Container %s in pod %s runs as root (UID 0)",
									container.Name, nmoPod.Object.Name))
						}

						if securityContext.AllowPrivilegeEscalation == nil || *securityContext.AllowPrivilegeEscalation {
							errorMessages = append(errorMessages,
								fmt.Sprintf(
									"Container %s in pod %s: AllowPrivilegeEscalation must be explicitly false",
									container.Name, nmoPod.Object.Name))
						}

						if securityContext.Capabilities == nil {
							errorMessages = append(errorMessages,
								fmt.Sprintf(
									"Container %s in pod %s: Capabilities block is nil, must drop ALL",
									container.Name, nmoPod.Object.Name))
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
										container.Name, nmoPod.Object.Name))
							}
						}

						seccompOk := false
						if securityContext.SeccompProfile != nil &&
							securityContext.SeccompProfile.Type == corev1.SeccompProfileTypeRuntimeDefault {
							seccompOk = true
						} else if nmoPod.Object.Spec.SecurityContext != nil &&
							nmoPod.Object.Spec.SecurityContext.SeccompProfile != nil &&
							nmoPod.Object.Spec.SecurityContext.SeccompProfile.Type ==
								corev1.SeccompProfileTypeRuntimeDefault {
							seccompOk = true
						}

						if !seccompOk {
							errorMessages = append(errorMessages,
								fmt.Sprintf(
									"Container %s in pod %s missing RuntimeDefault seccomp profile",
									container.Name, nmoPod.Object.Name))
						}
					}

					if !managerFound {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Pod %s has no container named %q",
								nmoPod.Object.Name, nmoparams.ManagerContainerName))
					}
				}

				if len(errorMessages) > 0 {
					errMsg := "Testing security context of NMO container failed due to:\n"
					for _, msg := range errorMessages {
						errMsg += fmt.Sprintf("- %s\n", msg)
					}

					Fail(errMsg)
				}
			})
	})
