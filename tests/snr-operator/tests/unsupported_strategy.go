package tests

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/snr-operator/internal/snrparams"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const unsupportedStrategy = "NodeDeletion"

var _ = Describe(
	"SNR Unsupported Strategy tests",
	Ordered,
	ContinueOnFailure,
	Label(snrparams.Label), func() {
		BeforeAll(func() {
			// These tests validate CRD/CEL admission rules enforced by the API server,
			// not the SNR controller. We verify the CRD exists rather than waiting
			// for operator readiness to avoid masking broken CRD validation on rollout.
			By("Verify SNR CRD exists and is established")

			snrCRDName := "selfnoderemediationconfigs." + snrparams.CRDGroup

			snrCRD := &apiextensionsv1.CustomResourceDefinition{}
			err := APIClient.Get(context.TODO(),
				client.ObjectKey{Name: snrCRDName},
				snrCRD)
			Expect(err).ToNot(HaveOccurred(),
				"SelfNodeRemediationConfig CRD %q not found", snrCRDName)
		})

		It("Verify SNR with unsupported NodeDeletion strategy is rejected",
			reportxml.ID("60877"),
			Label(labels.TierAcceptance, labels.DisruptionNonDestructive,
				labels.PlatformAny, labels.FrequencyPresubmit,
				labels.ComponentController), func() {
				By("Creating SNR with NodeDeletion remediationStrategy")

				snrCR := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": snrparams.CRDGroup + "/" + snrparams.CRDVersion,
						"kind":       "SelfNodeRemediation",
						"metadata": map[string]interface{}{
							"name":      "test-unsupported-snr",
							"namespace": medik8sparams.OperatorNs,
						},
						"spec": map[string]interface{}{
							"remediationStrategy": unsupportedStrategy,
						},
					},
				}

				err := APIClient.Create(context.TODO(), snrCR)
				if err == nil {
					deferDeleteCR(snrCR)
				}

				Expect(err).To(HaveOccurred(),
					"Creating SNR with NodeDeletion strategy should be rejected")
				Expect(err.Error()).To(ContainSubstring("Unsupported value"),
					"Error should mention unsupported value")
				Expect(err.Error()).To(ContainSubstring(unsupportedStrategy),
					"Error should mention the unsupported strategy")
			})

		It("Verify SNRT with unsupported NodeDeletion strategy is rejected",
			reportxml.ID("60822"),
			Label(labels.TierAcceptance, labels.DisruptionNonDestructive,
				labels.PlatformAny, labels.FrequencyPresubmit,
				labels.ComponentController), func() {
				By("Creating SelfNodeRemediationTemplate with NodeDeletion strategy")

				snrtCR := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": snrparams.CRDGroup + "/" + snrparams.CRDVersion,
						"kind":       "SelfNodeRemediationTemplate",
						"metadata": map[string]interface{}{
							"name":      "test-unsupported-snrt",
							"namespace": medik8sparams.OperatorNs,
						},
						"spec": map[string]interface{}{
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"remediationStrategy": unsupportedStrategy,
								},
							},
						},
					},
				}

				err := APIClient.Create(context.TODO(), snrtCR)
				if err == nil {
					deferDeleteCR(snrtCR)
				}

				Expect(err).To(HaveOccurred(),
					"Creating SNRT with NodeDeletion strategy should be rejected")
				Expect(err.Error()).To(ContainSubstring("Unsupported value"),
					"Error should mention unsupported value")
				Expect(err.Error()).To(ContainSubstring(unsupportedStrategy),
					"Error should mention the unsupported strategy")
			})
	})
