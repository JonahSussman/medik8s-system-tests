package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nmov1beta1 "github.com/medik8s/node-maintenance-operator/api/v1beta1"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/nodes"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/nmo-operator/internal/nmoparams"
)

const (
	schedulePodName = "nmo-schedule-test"
	schedulePodNs   = "default"
)

var _ = Describe(
	"NMO Maintenance Lifecycle",
	Ordered,
	ContinueOnFailure,
	Serial,
	Label(labels.OperatorNMO), func() {
		var (
			targetNodeName string
			nmCRName       string
		)

		BeforeAll(func() {
			By("Registering NMO API scheme")

			err := APIClient.AttachScheme(nmov1beta1.AddToScheme)
			Expect(err).ToNot(HaveOccurred(), "Failed to register NMO scheme")

			By("Verifying NMO deployment is Ready")

			nmoDeployment, err := deployment.Pull(
				APIClient, nmoparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get NMO deployment")
			Expect(nmoDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"NMO deployment is not Ready")

			By("Selecting a schedulable worker node")

			workerNodes, err := nodes.List(
				APIClient,
				metav1.ListOptions{LabelSelector: "node-role.kubernetes.io/worker"},
			)
			Expect(err).ToNot(HaveOccurred(), "Failed to list worker nodes")

			var eligible []*nodes.Builder

			for _, node := range workerNodes {
				nodeLabels := node.Object.Labels

				if _, hasMaster := nodeLabels["node-role.kubernetes.io/master"]; hasMaster {
					continue
				}

				if _, hasCP := nodeLabels["node-role.kubernetes.io/control-plane"]; hasCP {
					continue
				}

				if node.Object.Spec.Unschedulable {
					continue
				}

				if helpers.IsNodeReady(node.Object) {
					eligible = append(eligible, node)
				}
			}

			Expect(eligible).ToNot(BeEmpty(), "No eligible worker nodes found")
			Expect(len(eligible)).To(BeNumerically(">=", 2),
				"At least 2 schedulable worker nodes are required (one for maintenance, one for cluster health)")

			targetNodeName = eligible[0].Object.Name
			nmCRName = fmt.Sprintf("test-maintenance-%s", targetNodeName)

			By(fmt.Sprintf("Selected worker node: %s", targetNodeName))

			By("Cleaning up pre-existing schedule test pod if present")

			staleTestPod := &corev1.Pod{}

			err = APIClient.Get(context.Background(),
				client.ObjectKey{Name: schedulePodName, Namespace: schedulePodNs}, staleTestPod)

			switch {
			case err == nil:
				if delErr := APIClient.Delete(context.Background(), staleTestPod); delErr != nil {
					GinkgoWriter.Printf("WARNING: failed to delete stale schedule test pod: %v\n", delErr)
				}
			case !errors.IsNotFound(err):
				GinkgoWriter.Printf("WARNING: unexpected error checking stale schedule test pod: %v\n", err)
			}

			By("Verifying no pre-existing NodeMaintenance CR for target node")
			deleteAndWaitForNMCR(context.Background(), nmCRName, nmoparams.UncordonTimeout)
		})

		AfterAll(func() {
			By("Safety cleanup: removing NodeMaintenance CR if still exists")

			nmCleanup := &nmov1beta1.NodeMaintenance{}

			cleanupErr := APIClient.Get(context.Background(), client.ObjectKey{Name: nmCRName}, nmCleanup)

			switch {
			case cleanupErr == nil:
				if delErr := APIClient.Delete(context.Background(), nmCleanup); delErr != nil {
					GinkgoWriter.Printf("WARNING: failed to delete NodeMaintenance CR %s: %v\n",
						nmCRName, delErr)
				}
			case !errors.IsNotFound(cleanupErr):
				GinkgoWriter.Printf("WARNING: unexpected error checking NodeMaintenance CR %s: %v\n",
					nmCRName, cleanupErr)
			}

			By("Safety cleanup: removing schedule test pod if still exists")

			testPod := &corev1.Pod{}

			cleanupErr = APIClient.Get(context.Background(),
				client.ObjectKey{Name: schedulePodName, Namespace: schedulePodNs}, testPod)

			switch {
			case cleanupErr == nil:
				if delErr := APIClient.Delete(context.Background(), testPod); delErr != nil {
					GinkgoWriter.Printf("WARNING: failed to delete schedule test pod: %v\n", delErr)
				}
			case !errors.IsNotFound(cleanupErr):
				GinkgoWriter.Printf("WARNING: unexpected error checking schedule test pod: %v\n", cleanupErr)
			}

			if targetNodeName != "" {
				By("Waiting for target node to become Ready before uncordon check")

				if err := helpers.WaitForNodeReady(
					context.Background(), APIClient, targetNodeName,
					nmoparams.DefaultPollInterval, nmoparams.RebootTimeout,
					GinkgoWriter.Printf,
				); err != nil {
					GinkgoWriter.Printf("WARNING: node %s did not become Ready: %v\n", targetNodeName, err)
				}

				By("Verifying target node is uncordoned after cleanup")
				Eventually(func() bool {
					node, err := nodes.Pull(APIClient, targetNodeName)
					if err != nil {
						return false
					}

					return !node.Object.Spec.Unschedulable
				}, nmoparams.UncordonTimeout, nmoparams.DefaultPollInterval).Should(BeTrue(),
					"Target node is still cordoned after cleanup")
			}
		})

		It("Start node maintenance",
			reportxml.ID("29592"),
			Label(
				labels.OperatorNMO,
				labels.DisruptionDestructive,
				labels.TierAcceptance,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyWeekly,
			), func() {
				By(fmt.Sprintf("Creating NodeMaintenance CR for node %s", targetNodeName))
				nodeMaintenance := &nmov1beta1.NodeMaintenance{
					ObjectMeta: metav1.ObjectMeta{
						Name: nmCRName,
					},
					Spec: nmov1beta1.NodeMaintenanceSpec{
						NodeName: targetNodeName,
						Reason:   "system-tests lifecycle validation (RHWA-1250)",
					},
				}
				Expect(APIClient.Create(context.Background(), nodeMaintenance)).To(Succeed(),
					"Failed to create NodeMaintenance CR")

				By("Waiting for NodeMaintenance to reach Succeeded phase")
				Eventually(func() nmov1beta1.MaintenancePhase {
					current := &nmov1beta1.NodeMaintenance{}

					err := APIClient.Get(context.Background(), client.ObjectKey{Name: nmCRName}, current)
					if err != nil {
						return ""
					}

					return current.Status.Phase
				}, nmoparams.MaintenanceTimeout, nmoparams.DefaultPollInterval).Should(Equal(nmov1beta1.MaintenanceSucceeded),
					"NodeMaintenance did not reach Succeeded phase")

				By("Verifying target node is cordoned")

				node, err := nodes.Pull(APIClient, targetNodeName)
				Expect(err).ToNot(HaveOccurred())
				Expect(node.Object.Spec.Unschedulable).To(BeTrue(),
					"Node should be cordoned (Unschedulable=true)")
			})

		It("Schedule pod to node under maintenance",
			reportxml.ID("29603"),
			Label(
				labels.OperatorNMO,
				labels.DisruptionDestructive,
				labels.TierAcceptance,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyWeekly,
			), func() {
				By(fmt.Sprintf("Creating pod with nodeSelector targeting %s", targetNodeName))
				testPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      schedulePodName,
						Namespace: schedulePodNs,
					},
					Spec: corev1.PodSpec{
						NodeSelector: map[string]string{
							"kubernetes.io/hostname": targetNodeName,
						},
						Containers: []corev1.Container{{
							Name:    "sleep",
							Image:   nmoparams.WorkloadTestImage,
							Command: []string{"sleep", "3600"},
						}},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				}
				Expect(APIClient.Create(context.Background(), testPod)).To(Succeed(),
					"Failed to create schedule test pod")

				By("Verifying pod stays in Pending state (node is unschedulable)")
				Consistently(func() corev1.PodPhase {
					pod := &corev1.Pod{}

					err := APIClient.Get(context.Background(),
						client.ObjectKey{Name: schedulePodName, Namespace: schedulePodNs}, pod)
					if err != nil {
						return ""
					}

					return pod.Status.Phase
				}, nmoparams.ScheduleCheckTimeout, nmoparams.DefaultPollInterval).Should(Equal(corev1.PodPending),
					"Pod should remain Pending on a cordoned node")

				By("Verifying pod was not scheduled (no nodeName assigned)")

				pod := &corev1.Pod{}
				Expect(APIClient.Get(context.Background(),
					client.ObjectKey{Name: schedulePodName, Namespace: schedulePodNs}, pod)).To(Succeed())
				Expect(pod.Spec.NodeName).To(BeEmpty(),
					"Pod should not be assigned to any node")

				By("Cleaning up schedule test pod")
				Expect(APIClient.Delete(context.Background(), pod)).To(Succeed())
				Eventually(func() bool {
					err := APIClient.Get(context.Background(),
						client.ObjectKey{Name: schedulePodName, Namespace: schedulePodNs}, &corev1.Pod{})

					return errors.IsNotFound(err)
				}, nmoparams.ScheduleCheckTimeout, nmoparams.DefaultPollInterval).Should(BeTrue(),
					"Schedule test pod was not deleted")
			})

		It("Maintenance mode persists after node reboot",
			reportxml.ID("46761"),
			Label(
				labels.OperatorNMO,
				labels.DisruptionDestructive,
				labels.TierAcceptance,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyWeekly,
			), func() {
				By("Capturing boot ID before reboot")

				previousBootID, err := helpers.GetNodeBootIDFromAPI(
					context.Background(), APIClient, targetNodeName)
				Expect(err).ToNot(HaveOccurred(), "Failed to get boot ID before reboot")

				By(fmt.Sprintf("Rebooting node %s via oc debug", targetNodeName))
				_, _ = helpers.RunOnNode(context.Background(), targetNodeName, 2*time.Minute, "systemctl", "reboot")

				By("Waiting for node to reboot (boot ID change)")
				Expect(helpers.WaitForNodeReboot(
					context.Background(), APIClient, targetNodeName,
					previousBootID, nmoparams.DefaultPollInterval, nmoparams.RebootTimeout,
					GinkgoWriter.Printf,
				)).To(Succeed(), "Node did not reboot (boot ID unchanged)")

				By("Waiting for node to recover and become Ready")
				Expect(helpers.WaitForNodeReady(
					context.Background(), APIClient, targetNodeName,
					nmoparams.DefaultPollInterval, nmoparams.RebootTimeout,
					GinkgoWriter.Printf,
				)).To(Succeed(), "Node did not return to Ready state after reboot")

				By("Verifying NodeMaintenance CR still exists and phase is Succeeded")
				Eventually(func(g Gomega) {
					nm := &nmov1beta1.NodeMaintenance{}
					g.Expect(APIClient.Get(context.Background(), client.ObjectKey{Name: nmCRName}, nm)).To(Succeed(),
						"NodeMaintenance CR should still exist after reboot")
					g.Expect(nm.Status.Phase).To(Equal(nmov1beta1.MaintenanceSucceeded),
						"NodeMaintenance phase should still be Succeeded after reboot")
				}, nmoparams.MaintenanceTimeout, nmoparams.DefaultPollInterval).Should(Succeed())

				By("Verifying target node remains cordoned after reboot")
				Eventually(func(g Gomega) {
					node, err := nodes.Pull(APIClient, targetNodeName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(node.Object.Spec.Unschedulable).To(BeTrue(),
						"Node should remain cordoned after reboot")
				}, nmoparams.MaintenanceTimeout, nmoparams.DefaultPollInterval).Should(Succeed())
			})

		It("Stop node maintenance",
			reportxml.ID("29594"),
			Label(
				labels.OperatorNMO,
				labels.DisruptionDestructive,
				labels.TierAcceptance,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyWeekly,
			), func() {
				By(fmt.Sprintf("Deleting NodeMaintenance CR %s and waiting for removal", nmCRName))
				deleteAndWaitForNMCR(context.Background(), nmCRName, nmoparams.UncordonTimeout)

				By("Verifying target node is uncordoned")
				Eventually(func() bool {
					node, err := nodes.Pull(APIClient, targetNodeName)
					if err != nil {
						return false
					}

					return !node.Object.Spec.Unschedulable
				}, nmoparams.UncordonTimeout, nmoparams.DefaultPollInterval).Should(BeTrue(),
					"Node should be uncordoned after maintenance stop")
			})
	})

func deleteAndWaitForNMCR(ctx context.Context, name string, timeout time.Duration) {
	existing := &nmov1beta1.NodeMaintenance{}

	err := APIClient.Get(ctx, client.ObjectKey{Name: name}, existing)

	switch {
	case err == nil:
		Expect(APIClient.Delete(ctx, existing)).To(Succeed(),
			fmt.Sprintf("Failed to delete NodeMaintenance CR %s", name))
		Eventually(func() bool {
			err := APIClient.Get(ctx,
				client.ObjectKey{Name: name}, &nmov1beta1.NodeMaintenance{})

			return errors.IsNotFound(err)
		}, timeout, nmoparams.DefaultPollInterval).Should(BeTrue(),
			fmt.Sprintf("NodeMaintenance CR %s was not deleted in time", name))
	case errors.IsNotFound(err):
		return
	default:
		Expect(err).ToNot(HaveOccurred(),
			fmt.Sprintf("Unexpected error checking NodeMaintenance CR %s", name))
	}
}
