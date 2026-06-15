package tests

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/snr-operator/internal/snrparams"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const unsupportedStrategy = "NodeDeletion"

var _ = Describe(
	"SNR Unsupported Strategy tests",
	Ordered,
	ContinueOnFailure,
	Label(snrparams.Label), func() {
		BeforeAll(func() {
			By("Verify SNR deployment is ready")

			snrDeployment, err := deployment.Pull(
				APIClient, snrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get SNR deployment")
			Expect(snrDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"SNR deployment is not Ready")
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
