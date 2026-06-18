package tests

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/snr-operator/internal/snrparams"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// buildSNRCR builds an unstructured SNR custom resource of the given kind.
func buildSNRCR(kind, name string, spec map[string]interface{}) *unstructured.Unstructured {
	resource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": snrparams.CRDGroup + "/" + snrparams.CRDVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": medik8sparams.OperatorNs,
			},
		},
	}

	if spec != nil {
		resource.Object["spec"] = spec
	}

	return resource
}

// deferDeleteCR registers cleanup for a CR, retrying deletion with Eventually.
func deferDeleteCR(resource *unstructured.Unstructured) {
	DeferCleanup(func() {
		Eventually(func() error {
			deleteErr := APIClient.Delete(context.TODO(), resource)
			if k8serrors.IsNotFound(deleteErr) {
				return nil
			}

			return deleteErr
		}, medik8sparams.DefaultTimeout, snrparams.DefaultPollInterval).Should(Succeed(),
			"cleanup of test CR %q must succeed", resource.GetName())
	})
}
