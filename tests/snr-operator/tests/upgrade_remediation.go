package tests

import (
	"context"
	"fmt"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/snr-operator/internal/snrparams"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (hooks *snrUpgradeOperatorFBCTest) selectRemediationWorker(ctx context.Context) (*corev1.Node, error) {
	leaderNode, err := helpers.GetActiveControllerNode(ctx, APIClient, "547f6cb6.medik8s.io", hooks.owned.Name)
	if err != nil {
		return nil, err
	}
	excluded := []string{leaderNode}
	nodes := &corev1.NodeList{}
	if err := APIClient.List(ctx, nodes); err != nil {
		return nil, err
	}

	for _, node := range nodes.Items {
		_, master := node.Labels["node-role.kubernetes.io/master"]
		_, controlPlane := node.Labels["node-role.kubernetes.io/control-plane"]
		if master || controlPlane || node.Labels["remediation.medik8s.io/exclude-from-remediation"] == "true" {
			excluded = append(excluded, node.Name)
		}
	}

	return helpers.SelectWorkerNode(ctx, APIClient, excluded...)
}

func (hooks *snrUpgradeOperatorFBCTest) runRemediationCycle(ctx context.Context, phase, expectedImage string) error {
	node, err := hooks.selectRemediationWorker(ctx)
	if err != nil {
		return err
	}
	if node.Status.NodeInfo.BootID == "" {
		return fmt.Errorf("target worker %s has no boot ID", node.Name)
	}
	hooks.targetNode = node.Name
	Eventually(func() bool {
		current := &corev1.Node{}
		if err := APIClient.Get(ctx, client.ObjectKey{Name: node.Name}, current); err != nil {
			return false
		}

		return current.Annotations["is-reboot-capable.self-node-remediation.medik8s.io"] == "true"
	}, snrparams.NodeReadyTimeout, snrparams.DefaultPollInterval).Should(BeTrue())
	remediation := buildSNRCR("SelfNodeRemediation", node.Name,
		map[string]interface{}{"remediationStrategy": "OutOfServiceTaint"})
	remediation.SetLabels(map[string]string{helpers.FBCRunLabel: hooks.owned.Token})
	if err := APIClient.Create(ctx, remediation); err != nil {
		return err
	}
	hooks.remediation = remediation
	GinkgoWriter.Printf("SNR %s-upgrade remediation: node=%s uid=%s bootID=%s\n",
		phase, node.Name, node.UID, node.Status.NodeInfo.BootID)

	Eventually(func() error {
		current := &corev1.Node{}
		if err := APIClient.Get(ctx, client.ObjectKey{Name: node.Name}, current); err != nil {
			return err
		}
		if current.UID != node.UID || current.Status.NodeInfo.BootID == "" ||
			current.Status.NodeInfo.BootID == node.Status.NodeInfo.BootID {
			return fmt.Errorf("worker has not rebooted with its original identity")
		}
		live := remediation.DeepCopy()
		if err := APIClient.Get(ctx, client.ObjectKeyFromObject(live), live); err != nil {
			return err
		}
		if live.GetUID() != remediation.GetUID() {
			return fmt.Errorf("remediation identity changed")
		}

		return verifyConditionsByType(live, expectedCondition{
			conditionType: conditionSucceeded, status: "True", reason: "RemediationFinishedSuccessfully",
		})
	}, snrparams.SNRDeletionTimeout, snrparams.DefaultPollInterval).Should(Succeed())
	Expect(hooks.deleteOwnedRemediation(ctx)).To(Succeed())
	Expect(helpers.WaitForNodeReady(ctx, APIClient, node.Name,
		snrparams.DefaultPollInterval, snrparams.NodeReadyTimeout, GinkgoWriter.Printf)).To(Succeed())
	current := &corev1.Node{}
	Expect(APIClient.Get(ctx, client.ObjectKey{Name: node.Name}, current)).To(Succeed())
	Expect(current.UID).To(Equal(node.UID), "remediation must reboot, not replace, the worker")
	Expect(current.Status.NodeInfo.BootID).NotTo(Equal(node.Status.NodeInfo.BootID))
	assertSNRRemediationTaintsRemoved(current)
	hooks.waitForAgents(ctx, expectedImage, nil, hooks.configSpec)
	AddReportEntry("snr-"+phase+"-upgrade-remediation", map[string]interface{}{
		"node": node.Name, "nodeUID": node.UID, "beforeBootID": node.Status.NodeInfo.BootID,
		"afterBootID": current.Status.NodeInfo.BootID, "ready": true,
	})
	hooks.targetNode = ""

	return nil
}

func assertSNRRemediationTaintsRemoved(node *corev1.Node) {
	for _, taint := range node.Spec.Taints {
		Expect(taint.Key).NotTo(BeElementOf("remediation.medik8s.io/self-node-remediation", snrparams.OutOfServiceTaintKey),
			"SNR must remove its remediation taints after recovery")
	}
}

func (hooks *snrUpgradeOperatorFBCTest) deleteOwnedRemediation(ctx context.Context) error {
	if hooks.remediation == nil {
		return nil
	}
	if err := hooks.owned.VerifyOwner(ctx); err != nil {
		return err
	}
	object := hooks.remediation.DeepCopy()
	if err := APIClient.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
		if apierrors.IsNotFound(err) {
			hooks.remediation = nil

			return nil
		}

		return err
	}
	if object.GetUID() != hooks.remediation.GetUID() || object.GetLabels()[helpers.FBCRunLabel] != hooks.owned.Token {
		return fmt.Errorf("refusing to delete replaced/unowned remediation %s", object.GetName())
	}
	// Signal cancellation so even a failed/incomplete remediation can finalize.
	original := object.DeepCopy()
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[nhcTimedOutAnnotationKey] = nhcTimedOutAnnotationValue
	object.SetAnnotations(annotations)
	if err := APIClient.Patch(ctx, object,
		client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return err
	}
	uid := object.GetUID()
	if err := client.IgnoreNotFound(APIClient.Delete(ctx, object, client.Preconditions{UID: &uid})); err != nil {
		return err
	}
	if err := wait.PollUntilContextTimeout(ctx, snrparams.DefaultPollInterval,
		snrparams.RemediationCRDeletionTimeout, true, func(ctx context.Context) (bool, error) {
			err := APIClient.Get(ctx, client.ObjectKeyFromObject(object), object)

			return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
		}); err != nil {
		return err
	}
	hooks.remediation = nil

	return nil
}

// Recover leaves resources available for diagnostics while allowing a targeted worker to recover.
func (hooks *snrUpgradeOperatorFBCTest) Recover(ctx context.Context) {
	if err := hooks.deleteOwnedRemediation(ctx); err != nil {
		AddReportEntry("snr-upgrade-remediation-cleanup-failure", err.Error())
	}
	if hooks.targetNode == "" {
		return
	}
	if err := helpers.WaitForNodeReady(ctx, APIClient, hooks.targetNode,
		snrparams.DefaultPollInterval, snrparams.NodeReadyTimeout, GinkgoWriter.Printf); err != nil {
		AddReportEntry("snr-upgrade-node-recovery-failure", err.Error())
	}
}
