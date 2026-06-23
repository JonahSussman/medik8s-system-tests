package tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrparams"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// buildSBR returns an unstructured StorageBasedRemediation CR named after nodeName.
// The SBR operator identifies the target node by metadata.name; spec is intentionally empty.
func buildSBR(nodeName string) *unstructured.Unstructured {
	return buildSBRUnstructured("StorageBasedRemediation", nodeName, map[string]interface{}{})
}

// pullSBRCR fetches the named StorageBasedRemediation CR from the cluster.
func pullSBRCR(nodeName string) (*unstructured.Unstructured, error) {
	sbrObject := &unstructured.Unstructured{}
	sbrObject.SetAPIVersion(sbrparams.CRDGroup + "/" + sbrparams.CRDVersion)
	sbrObject.SetKind("StorageBasedRemediation")

	err := APIClient.Get(context.TODO(),
		types.NamespacedName{Name: nodeName, Namespace: medik8sparams.OperatorNs}, sbrObject)
	if err != nil {
		return nil, err
	}

	return sbrObject, nil
}

// cleanupSBRCR force-removes a StorageBasedRemediation CR by clearing finalizers first.
// Safe to call when the CR may already be gone.
func cleanupSBRCR(nodeName string) {
	sbrObject, err := pullSBRCR(nodeName)

	if k8serrors.IsNotFound(err) {
		return
	}

	if err != nil {
		GinkgoT().Logf("Warning: cleanup get StorageBasedRemediation/%s: %v", nodeName, err)

		return
	}

	if len(sbrObject.GetFinalizers()) > 0 {
		sbrObject.SetFinalizers(nil)

		if updateErr := APIClient.Update(context.TODO(), sbrObject); updateErr != nil &&
			!k8serrors.IsNotFound(updateErr) {
			GinkgoT().Logf("Warning: cleanup clear finalizers on StorageBasedRemediation/%s: %v",
				nodeName, updateErr)
		}
	}

	if deleteErr := APIClient.Delete(context.TODO(), sbrObject); deleteErr != nil &&
		!k8serrors.IsNotFound(deleteErr) {
		GinkgoT().Logf("Warning: cleanup delete StorageBasedRemediation/%s: %v", nodeName, deleteErr)
	}
}

// controllerPodNodes returns the set of node names that currently host SBR controller pods.
// The SBR reconciler skips fencing its own node (the node the controller pod runs on),
// so a CR targeting one of these nodes exercises a different code path.
func controllerPodNodes() map[string]bool {
	nodeSet := make(map[string]bool)

	pods, err := pod.List(APIClient, medik8sparams.OperatorNs,
		metav1.ListOptions{LabelSelector: sbrparams.OperatorControllerPodLabelSelector})
	if err != nil {
		Fail(fmt.Sprintf("failed to list SBR controller pods: %v", err))

		return nodeSet // unreachable — Fail panics
	}

	for _, controllerPod := range pods {
		if controllerPod.Object.Spec.NodeName != "" {
			nodeSet[controllerPod.Object.Spec.NodeName] = true
		}
	}

	return nodeSet
}

var _ = Describe(
	"SBR Functional — StorageBasedRemediation CR",
	Ordered,
	ContinueOnFailure,
	Label(sbrparams.Label), func() {
		var (
			targetNodeName string
			// setupSBRC is created in BeforeAll to ensure the agent DaemonSet is running.
			// The SBRRemediationReconciler runs inside agent pods (not the main operator),
			// so without an active SBRC the finalizer on StorageBasedRemediation CRs is never
			// added.  A minimal SBRC (no sharedStorageClass) is sufficient: it creates the
			// DaemonSet, agents start running and can reconcile SBR CRs, but the nodeManager
			// stays nil so no actual fencing is initiated.
			setupSBRC *unstructured.Unstructured
		)

		BeforeAll(func() {
			By("Pre-cleanup: removing any stale SBRC from prior runs")

			staleRef := buildSBRC(sbrparams.SBRCFunctionalTestName, map[string]interface{}{})
			if deleteErr := APIClient.Delete(context.TODO(), staleRef); deleteErr != nil &&
				!k8serrors.IsNotFound(deleteErr) {
				GinkgoT().Logf("Warning: pre-cleanup delete %s: %v",
					sbrparams.SBRCFunctionalTestName, deleteErr)
			}

			Eventually(func() error {
				getErr := APIClient.Get(context.TODO(),
					types.NamespacedName{
						Name:      sbrparams.SBRCFunctionalTestName,
						Namespace: medik8sparams.OperatorNs,
					},
					buildSBRC(sbrparams.SBRCFunctionalTestName, map[string]interface{}{}))
				if k8serrors.IsNotFound(getErr) {
					return nil
				}

				if getErr != nil {
					return getErr
				}

				return fmt.Errorf("SBRC %s still terminating", sbrparams.SBRCFunctionalTestName)
			}, sbrparams.SBRCReadyTimeout, sbrparams.DefaultPollInterval).Should(Succeed(),
				"Stale SBRC must be fully gone before recreating")

			By("Creating StorageBasedRemediationConfig with sharedStorageClass so agent DaemonSet starts")

			// sharedStorageClass is required: without it the controller never creates the
			// storage init job, so the agent DaemonSet is never created and waitForSBRCReady
			// times out. discoverRWXStorageClass auto-discovers CephFS or reads SBR_STORAGE_CLASS.
			storageClass := discoverRWXStorageClass()
			setupSBRC = buildSBRC(sbrparams.SBRCFunctionalTestName, map[string]interface{}{
				"sharedStorageClass": storageClass,
			})

			createErr := APIClient.Create(context.TODO(), setupSBRC)
			Expect(createErr).ToNot(HaveOccurred(),
				"StorageBasedRemediationConfig %q must be created before the remediation CR test",
				sbrparams.SBRCFunctionalTestName)

			waitForSBRCReady(sbrparams.SBRCFunctionalTestName)

			// Exclude nodes running SBR controller pods: the reconciler skips fencing its own
			// node (CR name == ownNodeName check), which would leave the CR in a state where
			// no conditions are ever set and the finalizer is never released on its own.
			controllerNodes := controllerPodNodes()

			nodeList, err := APIClient.CoreV1Interface.Nodes().List(context.TODO(), metav1.ListOptions{
				LabelSelector: "node-role.kubernetes.io/worker",
			})
			Expect(err).ToNot(HaveOccurred(), "Failed to list worker nodes")

			for nodeIdx := range nodeList.Items {
				node := &nodeList.Items[nodeIdx]
				if controllerNodes[node.Name] {
					GinkgoWriter.Printf("Skipping node %s (SBR controller pod runs there)\n", node.Name)

					continue
				}

				if isNodeSchedulable(node) {
					targetNodeName = node.Name

					break
				}
			}

			if targetNodeName == "" {
				Skip("No schedulable worker node available that does not host an SBR controller pod; " +
					"skipping StorageBasedRemediation CR lifecycle test")
			}

			GinkgoWriter.Printf("Target node for StorageBasedRemediation CR: %q\n", targetNodeName)
		})

		AfterAll(func() {
			if setupSBRC == nil {
				return
			}

			By("Removing StorageBasedRemediationConfig created for the remediation CR test")

			deleteErr := APIClient.Delete(context.TODO(), setupSBRC)
			if deleteErr != nil && !k8serrors.IsNotFound(deleteErr) {
				GinkgoT().Logf("Warning: cleanup delete StorageBasedRemediationConfig %s: %v",
					sbrparams.SBRCFunctionalTestName, deleteErr)
			}
		})

		It("Verify StorageBasedRemediation CR lifecycle: admission, finalizer, and deletion cleanup",
			reportxml.ID("88737"),
			Label(
				labels.OperatorSBR,
				labels.DisruptionDestructive,
				labels.TierAcceptance,
				labels.PlatformAny,
				labels.ComponentRemediation,
				labels.FrequencyNightly,
			), func() {
				// Register cleanup before creating: cleanupSBRCR is NotFound-safe, so
				// this is a no-op if creation fails, but ensures the CR is removed if
				// creation succeeds and a later assertion panics before any inline cleanup.
				DeferCleanup(func() {
					By("DeferCleanup: removing StorageBasedRemediation CR and waiting for node to be schedulable")

					cleanupSBRCR(targetNodeName)

					// Force-removing the finalizer via cleanupSBRCR bypasses handleDeletion in
					// the agent reconciler, which normally uncordons the node. Explicitly uncordon
					// to ensure the node is schedulable regardless of controller cleanup state.
					if nodeObj, getErr := APIClient.CoreV1Interface.Nodes().Get(
						context.TODO(), targetNodeName, metav1.GetOptions{}); getErr == nil &&
						nodeObj.Spec.Unschedulable {
						nodeObj.Spec.Unschedulable = false

						if _, updateErr := APIClient.CoreV1Interface.Nodes().Update(
							context.TODO(), nodeObj, metav1.UpdateOptions{}); updateErr != nil {
							GinkgoT().Logf("Warning: failed to uncordon node %s: %v",
								targetNodeName, updateErr)
						}
					}

					Eventually(func() error {
						node, nodeErr := APIClient.CoreV1Interface.Nodes().Get(
							context.TODO(), targetNodeName, metav1.GetOptions{})
						if nodeErr != nil {
							return fmt.Errorf("failed to get node %s: %w", targetNodeName, nodeErr)
						}

						if node.Spec.Unschedulable {
							return fmt.Errorf("node %s still cordoned after StorageBasedRemediation CR cleanup",
								targetNodeName)
						}

						return nil
					}, medik8sparams.DefaultTimeout, sbrparams.DefaultPollInterval).Should(Succeed(),
						"Node %s must not be cordoned after StorageBasedRemediation CR is removed", targetNodeName)
				})

				By(fmt.Sprintf("Creating StorageBasedRemediation CR targeting node %q", targetNodeName))

				sbrCR := buildSBR(targetNodeName)

				createErr := APIClient.Create(context.TODO(), sbrCR)
				Expect(createErr).ToNot(HaveOccurred(),
					"StorageBasedRemediation CR should be admitted by the API server (spec is intentionally empty)")

				By(fmt.Sprintf(
					"Verifying controller adds finalizer %q to the StorageBasedRemediation CR",
					sbrparams.SBRRemediationFinalizer))

				var liveCR *unstructured.Unstructured

				// Wait for the controller to add any finalizer (proof of reconcile).
				Eventually(func() error {
					sbrCRObj, pullErr := pullSBRCR(targetNodeName)
					if pullErr != nil {
						return pullErr
					}

					if len(sbrCRObj.GetFinalizers()) == 0 {
						return fmt.Errorf(
							"controller has not yet added any finalizer to StorageBasedRemediation/%s",
							targetNodeName)
					}

					liveCR = sbrCRObj

					return nil
				}, medik8sparams.DefaultTimeout, sbrparams.DefaultPollInterval).Should(Succeed(),
					"Controller must add a finalizer to StorageBasedRemediation/%s", targetNodeName)

				// Verify the exact finalizer string immediately — fail fast rather than
				// waiting DefaultTimeout when the constant is out of sync with the operator.
				Expect(liveCR.GetFinalizers()).To(ContainElement(sbrparams.SBRRemediationFinalizer),
					"StorageBasedRemediation/%s has unexpected finalizer(s) %v; "+
						"sbrparams.SBRRemediationFinalizer may be out of sync with the operator repo",
					targetNodeName, liveCR.GetFinalizers())

				// Informational only — not asserted. Conditions require an active agent pool.
				// Fresh pull because the controller sets conditions in a second reconcile iteration
				// (after the finalizer-add requeue); liveCR was captured in the first.
				if freshCR, pullErr := pullSBRCR(targetNodeName); pullErr == nil {
					conditions, _, _ := unstructured.NestedSlice(freshCR.Object, "status", "conditions")
					if len(conditions) > 0 {
						GinkgoWriter.Printf("StorageBasedRemediation/%s conditions: %v\n",
							targetNodeName, conditions)
					} else {
						GinkgoWriter.Printf(
							"StorageBasedRemediation/%s: no conditions set "+
								"(node manager likely not available — expected without active DaemonSet)\n",
							targetNodeName)
					}
				}

				By(fmt.Sprintf("Deleting StorageBasedRemediation CR for node %q", targetNodeName))

				deleteErr := APIClient.Delete(context.TODO(), liveCR)
				Expect(deleteErr).ToNot(HaveOccurred(),
					"StorageBasedRemediation CR deletion must succeed")

				By("Verifying controller releases the finalizer and the CR is fully removed")

				Eventually(func() error {
					_, getErr := pullSBRCR(targetNodeName)
					if k8serrors.IsNotFound(getErr) {
						return nil
					}

					if getErr != nil {
						return getErr
					}

					return fmt.Errorf(
						"StorageBasedRemediation/%s still exists; waiting for controller to release finalizer %q",
						targetNodeName, sbrparams.SBRRemediationFinalizer)
				}, medik8sparams.DefaultTimeout, sbrparams.DefaultPollInterval).Should(Succeed(),
					"StorageBasedRemediation/%s must be fully removed after controller releases finalizer %q",
					targetNodeName, sbrparams.SBRRemediationFinalizer)
			})
	})
