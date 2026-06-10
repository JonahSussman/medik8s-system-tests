package tests

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/infrastructure"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"
	oplmV1alpha1 "github.com/rh-ecosystem-edge/eco-goinfra/pkg/schemes/olm/operators/v1alpha1"

	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/snr-operator/internal/snrparams"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe(
	"SNR Post Deployment tests",
	Ordered,
	ContinueOnFailure,
	Label(snrparams.Label), func() {
		var (
			snrDeployment *deployment.Builder
			snrCSV        *olm.ClusterServiceVersionBuilder
		)

		BeforeAll(func() {
			By("Get SNR deployment object")

			var err error

			snrDeployment, err = deployment.Pull(
				APIClient, snrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get SNR deployment")

			By("Verify SNR deployment is Ready")
			Expect(snrDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(), "SNR deployment is not Ready")

			By("Get SNR ClusterServiceVersion")

			snrCSVs, err := olm.ListClusterServiceVersionWithNamePattern(
				APIClient, snrparams.CSVNamePattern, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to list SNR ClusterServiceVersions")
			Expect(len(snrCSVs)).To(BeNumerically(">", 0),
				"At least one SNR ClusterServiceVersion should be found")

			for _, csv := range snrCSVs {
				if csv.Object.Status.Phase == oplmV1alpha1.CSVPhaseSucceeded {
					snrCSV = csv

					break
				}
			}

			Expect(snrCSV).ToNot(BeNil(), "No SNR CSV in Succeeded phase found")
		})

		It("Verify SNR resources are installed and running",
			reportxml.ID("54205"), func() {
				By("Verifying SelfNodeRemediationConfig exists")

				snrcObj := &unstructured.Unstructured{}
				snrcObj.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   snrparams.CRDGroup,
					Version: snrparams.CRDVersion,
					Kind:    "SelfNodeRemediationConfig",
				})

				err := APIClient.Get(context.TODO(),
					client.ObjectKey{
						Name:      snrparams.SNRConfigName,
						Namespace: medik8sparams.OperatorNs,
					},
					snrcObj)
				Expect(err).ToNot(HaveOccurred(),
					"SelfNodeRemediationConfig %q not found", snrparams.SNRConfigName)

				By("Verifying SNR DaemonSet pods are running")

				dsPods, err := pod.List(APIClient, medik8sparams.OperatorNs, metav1.ListOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to list pods in operator namespace")

				var dsPodsCount int

				for _, p := range dsPods {
					if strings.HasPrefix(p.Object.Name, snrparams.DaemonSetPodPrefix) {
						dsPodsCount++
					}
				}

				Expect(dsPodsCount).To(BeNumerically(">", 0),
					"At least one SNR DaemonSet pod should be running")

				By("Verifying SNR controller-manager pods are running")

				listOptions := metav1.ListOptions{
					LabelSelector: snrparams.OperatorControllerPodLabelSelector,
				}

				_, err = pod.WaitForAllPodsInNamespaceRunning(
					APIClient,
					medik8sparams.OperatorNs,
					medik8sparams.DefaultTimeout,
					listOptions,
				)
				Expect(err).ToNot(HaveOccurred(), "SNR controller pods are not running")
			})

		It("Verify only Automatic remediation template exists",
			reportxml.ID("71010"), func() {
				By("Verifying Automatic SelfNodeRemediationTemplate exists")

				automaticTemplate := &unstructured.Unstructured{}
				automaticTemplate.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   snrparams.CRDGroup,
					Version: snrparams.CRDVersion,
					Kind:    "SelfNodeRemediationTemplate",
				})

				err := APIClient.Get(context.TODO(),
					client.ObjectKey{
						Name:      snrparams.SNRTemplateName,
						Namespace: medik8sparams.OperatorNs,
					},
					automaticTemplate)
				Expect(err).ToNot(HaveOccurred(),
					"Automatic SelfNodeRemediationTemplate %q not found", snrparams.SNRTemplateName)

				By("Verifying remediation strategy is Automatic")

				strategy, found, err := unstructured.NestedString(
					automaticTemplate.Object,
					"spec", "template", "spec", "remediationStrategy")
				Expect(err).ToNot(HaveOccurred(), "Failed to get remediationStrategy field")
				Expect(found).To(BeTrue(), "remediationStrategy field not found in template spec")
				Expect(strategy).To(Equal(snrparams.SNRTemplateExpectedStrategy),
					"Expected remediationStrategy %q, got %q",
					snrparams.SNRTemplateExpectedStrategy, strategy)

				By("Verifying unsupported templates do not exist")

				for _, unsupported := range snrparams.UnsupportedTemplateNames {
					tmpl := &unstructured.Unstructured{}
					tmpl.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   snrparams.CRDGroup,
						Version: snrparams.CRDVersion,
						Kind:    "SelfNodeRemediationTemplate",
					})

					err := APIClient.Get(context.TODO(),
						client.ObjectKey{
							Name:      unsupported,
							Namespace: medik8sparams.OperatorNs,
						},
						tmpl)
					Expect(k8serrors.IsNotFound(err)).To(BeTrue(),
						"Unsupported template %q should not exist", unsupported)
				}
			})

		It("Verify SNR CSV annotations",
			reportxml.ID("52136"), func() {
				By("Checking valid-subscription annotation")

				annotations := snrCSV.Object.Annotations
				Expect(annotations).ToNot(BeNil(), "CSV annotations should not be nil")

				_, hasValidSubscription := annotations["operators.openshift.io/valid-subscription"]
				Expect(hasValidSubscription).To(BeTrue(),
					"CSV should have operators.openshift.io/valid-subscription annotation")

				By("Checking support annotation")

				_, hasSupport := annotations["support"]
				Expect(hasSupport).To(BeTrue(), "CSV should have support annotation")

				By("Checking repository annotation")

				_, hasRepository := annotations["repository"]
				Expect(hasRepository).To(BeTrue(), "CSV should have repository annotation")

				By("Checking maintainers")

				maintainers := snrCSV.Object.Spec.Maintainers
				Expect(len(maintainers)).To(BeNumerically(">", 0),
					"CSV should have at least one maintainer")
			})

		It("Verify SNR CSV metadata",
			reportxml.ID("70705"), func() {
				By("Checking infrastructure feature annotations")

				annotations := snrCSV.Object.Annotations
				Expect(annotations).ToNot(BeNil(), "CSV annotations should not be nil")

				for annotationKey, expectedValue := range snrparams.RequiredAnnotations {
					annotationValue, exists := annotations[annotationKey]
					Expect(exists).To(BeTrue(),
						"Required annotation %q should exist on SNR CSV", annotationKey)
					Expect(annotationValue).To(Equal(expectedValue),
						"Annotation %q should have value %q, got %q",
						annotationKey, expectedValue, annotationValue)
				}

				By("Checking replaces field")

				replaces := snrCSV.Object.Spec.Replaces
				Expect(replaces).ToNot(BeEmpty(), "CSV spec.replaces should not be empty")
				Expect(replaces).To(ContainSubstring(snrparams.CSVNamePattern),
					"replaces field should contain %q, got %q", snrparams.CSVNamePattern, replaces)

				By("Checking cluster topology for replica validation")

				infraConfig, err := infrastructure.Pull(APIClient)
				Expect(err).ToNot(HaveOccurred(), "Failed to pull infrastructure configuration")

				if infraConfig.Object.Status.ControlPlaneTopology != configv1.SingleReplicaTopologyMode {
					By("Verifying controller replicas on multi-node cluster")

					Expect(snrDeployment.Object.Spec.Replicas).ToNot(BeNil(),
						"Deployment replicas should not be nil")
					Expect(*snrDeployment.Object.Spec.Replicas).To(Equal(snrparams.ExpectedReplicas),
						"Expected %d replica(s), found %d",
						snrparams.ExpectedReplicas, *snrDeployment.Object.Spec.Replicas)
					Expect(snrDeployment.Object.Status.ReadyReplicas).To(Equal(snrparams.ExpectedReplicas),
						"Expected %d ready replica(s), found %d",
						snrparams.ExpectedReplicas, snrDeployment.Object.Status.ReadyReplicas)
				}
			})
	})
