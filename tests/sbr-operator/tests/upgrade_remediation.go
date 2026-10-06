package tests

import (
	"context"
	"fmt"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrparams"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// runRemediationCycle adapts Emily's reboot/recovery check from PR #11.
// It is explicitly opt-in because it needs existing RWX storage and fences a node.
func (hooks *sbrUpgradeOperatorFBCTest) runRemediationCycle(ctx context.Context) error {
	By("selecting a healthy worker without an SBR controller for optional remediation")
	nodeName := pickTargetWorkerNode()
	if nodeName == "" {
		return fmt.Errorf("SBR remediation needs a schedulable worker without an SBR controller")
	}
	workers, err := APIClient.CoreV1Interface.Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "node-role.kubernetes.io/worker",
	})
	if err != nil {
		return err
	}
	healthyWorkers := 0
	for nodeIdx := range workers.Items {
		if isNodeSchedulable(&workers.Items[nodeIdx]) {
			healthyWorkers++
		}
	}
	if healthyWorkers < 2 {
		return fmt.Errorf("SBR fencing requires at least two healthy workers, got %d", healthyWorkers)
	}
	hooks.targetNode = nodeName

	By("creating an owned RWX-backed config and waiting for target and peer agents")
	// Agents cannot fence themselves: a peer must be running to write the fence
	// message. SBR's selector is a flat map, not a matchLabels structure.
	configName := "sbr-remediation-" + hooks.owned.Token
	config := buildSBRC(configName, map[string]interface{}{
		"sharedStorageClass": hooks.remediation.StorageClass,
		"nodeSelector":       map[string]interface{}{"node-role.kubernetes.io/worker": ""},
	})
	if err := hooks.owned.Create(ctx, config); err != nil {
		return err
	}
	waitForSBRCReady(configName)
	Eventually(func() error {
		agentDS, err := APIClient.DaemonSets(hooks.owned.Namespace).Get(
			ctx, sbrparams.SBRAgentDaemonSetPrefix+configName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if agentDS.Status.NumberReady < 2 {
			return fmt.Errorf("need ready target and peer SBR agents, got %d", agentDS.Status.NumberReady)
		}

		return nil
	}, sbrparams.SBRCReadyTimeout, sbrparams.DefaultPollInterval).Should(Succeed())

	By(fmt.Sprintf("creating an owned SBR remediation for worker %s", nodeName))
	bootID, err := getNodeBootID(nodeName)
	if err != nil {
		return err
	}
	if err := hooks.owned.Create(ctx, buildSBR(nodeName)); err != nil {
		return err
	}
	By("requiring a changed node boot ID, not just a successful CR creation")
	var newBootID string
	Eventually(func() error {
		var err error
		newBootID, err = getNodeBootID(nodeName)
		if err != nil {
			return err
		}
		if newBootID == bootID {
			return fmt.Errorf("worker %s has not rebooted (boot ID %s)", nodeName, bootID)
		}

		return nil
	}, sbrparams.NodeRebootTimeout, sbrparams.NodeRebootPollInterval).Should(Succeed())
	By("requiring the remediated worker to return to Ready")
	if err := helpers.WaitForNodeReady(ctx, APIClient, nodeName,
		sbrparams.NodeRebootPollInterval, sbrparams.NodeRebootTimeout, GinkgoWriter.Printf); err != nil {
		return err
	}
	AddReportEntry("sbr-upgrade-remediation", map[string]interface{}{
		"node": nodeName, "bootIDBefore": bootID, "bootIDAfter": newBootID, "ready": true,
	})
	GinkgoWriter.Printf("SBR remediation completed: node=%s bootID=%s -> %s Ready=true\n", nodeName, bootID, newBootID)

	return nil
}
