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
	"github.com/medik8s/system-tests/tests/mdr-operator/internal/mdrparams"

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

	for _, mdrPod := range pods {
		if mdrPod.Object.Status.Phase != corev1.PodRunning || mdrPod.Object.DeletionTimestamp != nil {
			continue
		}

		allReady := true

		for _, cs := range mdrPod.Object.Status.ContainerStatuses {
			if !cs.Ready {
				allReady = false

				break
			}
		}

		if allReady {
			running = append(running, mdrPod)
		}
	}

	return running
}

func fetchActiveCSV() *olm.ClusterServiceVersionBuilder {
	var mdrCSV *olm.ClusterServiceVersionBuilder

	Eventually(func() error {
		mdrCSVs, err := olm.ListClusterServiceVersionWithNamePattern(
			APIClient, mdrparams.CSVNamePattern, medik8sparams.OperatorNs)
		if err != nil {
			return fmt.Errorf("failed to list MDR ClusterServiceVersions: %w", err)
		}

		if len(mdrCSVs) == 0 {
			return fmt.Errorf("no MDR ClusterServiceVersion found in namespace %s", medik8sparams.OperatorNs)
		}

		mdrCSV = findActiveCSV(mdrCSVs)
		if mdrCSV == nil {
			return fmt.Errorf("no MDR CSV in Succeeded phase found yet")
		}

		return nil
	}, medik8sparams.DefaultTimeout, mdrparams.DefaultPollInterval).Should(Succeed(),
		"MDR CSV must reach Succeeded phase")

	return mdrCSV
}

var _ = Describe(
	"MDR Post Deployment tests",
	Ordered,
	ContinueOnFailure,
	Label(mdrparams.Label), func() {
		var controlPlaneTopology configv1.TopologyMode

		BeforeAll(func() {
			By("Get MDR deployment object and verify it is Ready")

			mdrDeployment, err := deployment.Pull(
				APIClient, mdrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get MDR deployment")
			Expect(mdrDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"MDR deployment is not Ready")

			By("Pull cluster topology for use in topology-aware tests")

			infraConfig, infraErr := infrastructure.Pull(APIClient)
			Expect(infraErr).ToNot(HaveOccurred(), "Failed to pull infrastructure configuration")

			controlPlaneTopology = infraConfig.Object.Status.ControlPlaneTopology
		})

		It("Verify Machine Deletion Remediation Operator pod is running",
			reportxml.ID("65767"),
			Label(
				labels.DisruptionNonDestructive,
				labels.TierSmoke,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyPresubmit,
			), func() {
				expectedCount := mdrparams.ExpectedReplicas
				if controlPlaneTopology == configv1.SingleReplicaTopologyMode {
					expectedCount = int32(1)
				}

				listOptions := metav1.ListOptions{LabelSelector: mdrparams.OperatorControllerPodLabelSelector}

				By("Verifying pod count matches expected replicas")

				Eventually(func() error {
					mdrPods, listErr := pod.List(APIClient, medik8sparams.OperatorNs, listOptions)
					if listErr != nil {
						return listErr
					}

					for _, mdrPod := range mdrPods {
						if mdrPod.Object.DeletionTimestamp != nil {
							continue
						}

						if mdrPod.Object.Status.Phase != corev1.PodRunning {
							return fmt.Errorf("pod %s is in phase %s, expected Running",
								mdrPod.Object.Name, mdrPod.Object.Status.Phase)
						}
					}

					runningCount := int32(len(filterRunningPods(mdrPods)))

					if runningCount != expectedCount {
						return fmt.Errorf("expected %d running MDR pod(s), found %d",
							expectedCount, runningCount)
					}

					return nil
				}, medik8sparams.DefaultTimeout, mdrparams.DefaultPollInterval).Should(Succeed(),
					"MDR pods did not reach expected running count of %d", expectedCount)
			})

		It("Verify MDR CSV has required annotations",
			reportxml.ID("65768"),
			Label(
				labels.DisruptionNonDestructive,
				labels.TierSmoke,
				labels.PlatformAny,
				labels.ComponentOLM,
				labels.FrequencyPresubmit,
			), func() {
				By("Finding the active (Succeeded) CSV")

				mdrCSV := fetchActiveCSV()

				By("Checking annotation values on MDR CSV")

				Expect(mdrCSV.Object.Annotations).ToNot(BeNil(), "CSV annotations should not be nil")

				var annotationErrors []string

				for annotationKey, expectedValue := range mdrparams.RequiredAnnotations {
					annotationValue, exists := mdrCSV.Object.Annotations[annotationKey]
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
					errMsg := "MDR CSV annotation validation failures:\n"
					for _, msg := range annotationErrors {
						errMsg += fmt.Sprintf("- %s\n", msg)
					}

					Fail(errMsg)
				}
			})

		It("Verify MDR controller manager has correct number of replicas",
			reportxml.ID("65769"),
			Label(
				labels.DisruptionNonDestructive,
				labels.TierSmoke,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyPresubmit,
			), func() {
				By("Checking cluster topology")

				if controlPlaneTopology == configv1.SingleReplicaTopologyMode {
					Skip("Skipping test on SNO (Single Node OpenShift) cluster")
				}

				By("Verifying replica count, ready replicas, and pod HA distribution")

				listOptions := metav1.ListOptions{LabelSelector: mdrparams.OperatorControllerPodLabelSelector}

				Eventually(func() error {
					liveDeploy, pullErr := deployment.Pull(
						APIClient, mdrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
					if pullErr != nil {
						return pullErr
					}

					if liveDeploy.Object.Spec.Replicas == nil ||
						*liveDeploy.Object.Spec.Replicas != mdrparams.ExpectedReplicas {
						desired := int32(0)
						if liveDeploy.Object.Spec.Replicas != nil {
							desired = *liveDeploy.Object.Spec.Replicas
						}

						return fmt.Errorf("expected %d desired replica(s), found %d",
							mdrparams.ExpectedReplicas, desired)
					}

					if liveDeploy.Object.Status.ReadyReplicas != mdrparams.ExpectedReplicas {
						return fmt.Errorf("expected %d ready replica(s), found %d",
							mdrparams.ExpectedReplicas, liveDeploy.Object.Status.ReadyReplicas)
					}

					mdrPods, listErr := pod.List(APIClient, medik8sparams.OperatorNs, listOptions)
					if listErr != nil {
						return listErr
					}

					runningPods := filterRunningPods(mdrPods)

					if len(runningPods) != int(mdrparams.ExpectedReplicas) {
						return fmt.Errorf("expected %d running MDR pod(s) for HA check, found %d",
							mdrparams.ExpectedReplicas, len(runningPods))
					}

					nodeNames := make(map[string]bool)

					for _, p := range runningPods {
						if p.Object.Spec.NodeName == "" {
							return fmt.Errorf("pod %s has not been assigned to a node", p.Object.Name)
						}

						nodeNames[p.Object.Spec.NodeName] = true
					}

					if len(nodeNames) != int(mdrparams.ExpectedReplicas) {
						return fmt.Errorf(
							"MDR pods must run on different nodes for HA, found pods on %d unique node(s)",
							len(nodeNames))
					}

					return nil
				}, medik8sparams.DefaultTimeout, mdrparams.DefaultPollInterval).Should(Succeed(),
					"MDR deployment did not stabilise at %d ready replicas on distinct nodes",
					mdrparams.ExpectedReplicas)
			})

		It("Verify MDR container runs as non-root user",
			reportxml.ID("65770"),
			Label(
				labels.DisruptionNonDestructive,
				labels.TierSmoke,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyPresubmit,
			), func() {
				By("Getting MDR controller pod names")

				listOptions := metav1.ListOptions{LabelSelector: mdrparams.OperatorControllerPodLabelSelector}

				var runningPods []*pod.Builder

				Eventually(func() error {
					mdrPods, listErr := pod.List(APIClient, medik8sparams.OperatorNs, listOptions)
					if listErr != nil {
						return listErr
					}

					running := filterRunningPods(mdrPods)
					if len(running) == 0 {
						return fmt.Errorf("no running MDR controller pods found")
					}

					runningPods = running

					return nil
				}, medik8sparams.DefaultTimeout, mdrparams.DefaultPollInterval).Should(Succeed(),
					"At least one running MDR controller pod should be found")

				var errorMessages []string

				for _, mdrPod := range runningPods {
					By(fmt.Sprintf("Verifying security context for pod %s", mdrPod.Object.Name))

					By("Checking pod-level runAsNonRoot security context")

					if mdrPod.Object.Spec.SecurityContext == nil {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Pod %s has nil SecurityContext", mdrPod.Object.Name))
					} else if mdrPod.Object.Spec.SecurityContext.RunAsNonRoot == nil {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Pod %s has nil runAsNonRoot", mdrPod.Object.Name))
					} else if !*mdrPod.Object.Spec.SecurityContext.RunAsNonRoot {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Incorrect runAsNonRoot for pod %s. Expected true, found: %v",
								mdrPod.Object.Name,
								*mdrPod.Object.Spec.SecurityContext.RunAsNonRoot))
					}

					By("Checking manager container security context")

					managerFound := false

					for _, container := range mdrPod.Object.Spec.Containers {
						if container.Name != mdrparams.ManagerContainerName {
							continue
						}

						managerFound = true
						securityContext := container.SecurityContext

						if securityContext == nil {
							errorMessages = append(errorMessages,
								fmt.Sprintf("Container %s in pod %s has nil SecurityContext",
									container.Name, mdrPod.Object.Name))

							continue
						}

						if securityContext.RunAsUser != nil && *securityContext.RunAsUser == 0 {
							errorMessages = append(errorMessages,
								fmt.Sprintf("Container %s in pod %s runs as root (UID 0)",
									container.Name, mdrPod.Object.Name))
						}

						if securityContext.AllowPrivilegeEscalation == nil || *securityContext.AllowPrivilegeEscalation {
							errorMessages = append(errorMessages,
								fmt.Sprintf(
									"Container %s in pod %s: AllowPrivilegeEscalation must be explicitly false",
									container.Name, mdrPod.Object.Name))
						}

						if securityContext.Capabilities == nil {
							errorMessages = append(errorMessages,
								fmt.Sprintf(
									"Container %s in pod %s: Capabilities block is nil, must drop ALL",
									container.Name, mdrPod.Object.Name))
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
										container.Name, mdrPod.Object.Name))
							}
						}
					}

					if !managerFound {
						errorMessages = append(errorMessages,
							fmt.Sprintf("Pod %s has no container named %q",
								mdrPod.Object.Name, mdrparams.ManagerContainerName))
					}
				}

				if len(errorMessages) > 0 {
					errMsg := "Testing security context of MDR container failed due to:\n"
					for _, msg := range errorMessages {
						errMsg += fmt.Sprintf("- %s\n", msg)
					}

					Fail(errMsg)
				}
			})
	})
