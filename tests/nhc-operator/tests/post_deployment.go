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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe(
	"NHC Post Deployment tests",
	Ordered,
	ContinueOnFailure,
	Label(nhcparams.Label), func() {
		var nhcCSV *olm.ClusterServiceVersionBuilder

		BeforeAll(func() {
			By("Get NHC deployment object and verify readiness")

			nhcDeployment, err := deployment.Pull(
				APIClient, nhcparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get NHC deployment")
			Expect(nhcDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(), "NHC deployment is not Ready")

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

				for _, csv := range nhcCSVs {
					phase, phaseErr := csv.GetPhase()
					if phaseErr == nil && phase == "Succeeded" {
						nhcCSV = csv

						return nil
					}
				}

				return fmt.Errorf("no NHC CSV in Succeeded phase found yet")
			}, medik8sparams.DefaultTimeout, nhcparams.DefaultPollInterval).Should(Succeed(),
				"NHC CSV must reach Succeeded phase")
		})

		It("Verify NHC resources are installed and running",
			reportxml.ID("89550"),
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
			reportxml.ID("89551"),
			Label(labels.OperatorNHC, labels.TierSmoke, labels.DisruptionNonDestructive,
				labels.PlatformAny, labels.FrequencyPresubmit,
				labels.ComponentOLM), func() {
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
			reportxml.ID("89552"),
			Label(labels.OperatorNHC, labels.TierSmoke, labels.DisruptionNonDestructive,
				labels.PlatformAny, labels.FrequencyPresubmit,
				labels.ComponentOLM), func() {
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

				By("Checking replaces field")

				replaces := nhcCSV.Object.Spec.Replaces
				Expect(replaces).ToNot(BeEmpty(), "CSV spec.replaces should not be empty")
				Expect(replaces).To(ContainSubstring(nhcparams.CSVNamePattern),
					"replaces field should contain %q, got %q", nhcparams.CSVNamePattern, replaces)

				By("Checking cluster topology for replica validation")

				infraConfig, err := infrastructure.Pull(APIClient)
				Expect(err).ToNot(HaveOccurred(), "Failed to pull infrastructure configuration")

				if infraConfig.Object.Status.ControlPlaneTopology == configv1.SingleReplicaTopologyMode {
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
	})
