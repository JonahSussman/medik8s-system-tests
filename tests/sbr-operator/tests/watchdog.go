package tests

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrparams"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe(
	"SBR Functional — Watchdog Integration",
	Ordered,
	ContinueOnFailure,
	Label(sbrparams.Label), func() {
		// nodeWatchdogDevices maps node name → discovered /dev/watchdog* paths.
		// Populated in BeforeAll from the shared inventory (if it already ran)
		// or from an independent per-node probe so this suite is self-contained.
		// Only Ready, schedulable nodes are included.
		var nodeWatchdogDevices map[string][]string

		BeforeAll(func() {
			nodeList, err := APIClient.CoreV1Interface.Nodes().List(context.TODO(), metav1.ListOptions{})
			Expect(err).ToNot(HaveOccurred(), "Failed to list cluster nodes for watchdog integration checks")
			Expect(nodeList.Items).ToNot(BeEmpty(), "No nodes found in the cluster")

			// Fast path: the 'SBR Debug — Cluster Watchdog Inventory' suite already
			// populated the shared map. Copy only schedulable nodes' entries so we
			// don't attempt to probe NotReady nodes and get misleading errors.
			if WatchdogDevicesByNode != nil {
				nodeWatchdogDevices = make(map[string][]string)

				for i := range nodeList.Items {
					node := &nodeList.Items[i]
					if !isNodeSchedulable(node) {
						GinkgoWriter.Printf("Skipping unschedulable/NotReady node %s\n", node.Name)

						continue
					}

					devs, ok := WatchdogDevicesByNode[node.Name]
					if !ok {
						GinkgoWriter.Printf("Warning: node %s missing from watchdog inventory; treating as no hardware watchdog\n",
							node.Name)

						devs = []string{}
					}

					nodeWatchdogDevices[node.Name] = devs
				}

				if len(nodeWatchdogDevices) == 0 {
					Skip("No schedulable nodes found in watchdog inventory; skipping test")
				}

				return
			}

			// Slow path: the inventory suite did not run (e.g. nightly label filter
			// excludes FrequencyPresubmit). Discover watchdog devices independently.
			nodeWatchdogDevices = make(map[string][]string)

			for i := range nodeList.Items {
				node := &nodeList.Items[i]
				nodeName := node.Name

				if !isNodeSchedulable(node) {
					GinkgoWriter.Printf("Skipping unschedulable/NotReady node %s\n", nodeName)

					continue
				}

				podName := watchdogDebugPodName(nodeName)

				debugPod, createErr := pod.NewBuilder(
					APIClient, podName, medik8sparams.OperatorNs, sbrparams.WatchdogDebugImage).
					DefineOnNode(nodeName).
					WithHostPid(true).
					WithPrivilegedFlag().
					CreateAndWaitUntilRunning(medik8sparams.DefaultTimeout)
				if createErr != nil {
					GinkgoWriter.Printf("Warning: could not create watchdog probe pod for node %s: %v\n",
						nodeName, createErr)
					nodeWatchdogDevices[nodeName] = nil

					continue
				}

				DeferCleanup(func(name string) {
					if existing, pullErr := pod.Pull(APIClient, name, medik8sparams.OperatorNs); pullErr == nil {
						if _, delErr := existing.Delete(); delErr != nil {
							GinkgoWriter.Printf("DeferCleanup: failed to delete probe pod %s: %v\n",
								name, delErr)
						}
					}
				}, podName)

				buf, execErr := debugPod.ExecCommand(
					[]string{"sh", "-c", "ls /proc/1/root/dev/watchdog* 2>/dev/null || true"})

				if _, delErr := debugPod.Delete(); delErr != nil {
					GinkgoWriter.Printf("Warning: failed to delete watchdog probe pod for node %s: %v\n",
						nodeName, delErr)
				}

				if execErr != nil {
					GinkgoWriter.Printf("Warning: exec failed on node %s: %v\n", nodeName, execErr)
					nodeWatchdogDevices[nodeName] = nil

					continue
				}

				// Use a non-nil empty slice so that "no devices found" is
				// distinguishable from "probe failed" (which leaves nil in the map).
				devices := make([]string, 0)

				for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
					for _, token := range strings.Fields(line) {
						if token == "" {
							continue
						}

						devices = append(devices, strings.TrimPrefix(token, "/proc/1/root"))
					}
				}

				nodeWatchdogDevices[nodeName] = devices
			}

			if len(nodeWatchdogDevices) == 0 {
				Skip("No schedulable nodes found; skipping watchdog integration test")
			}

			GinkgoWriter.Println("=== Watchdog inventory (self-discovered) ===")

			for nodeName, devs := range nodeWatchdogDevices {
				switch {
				case devs == nil:
					GinkgoWriter.Printf("  %s: probe-failed\n", nodeName)
				case len(devs) == 0:
					GinkgoWriter.Printf("  %s: none\n", nodeName)
				default:
					GinkgoWriter.Printf("  %s: %v\n", nodeName, devs)
				}
			}
		})

		It("Verify watchdog device accessibility and softdog module availability",
			reportxml.ID("88878"),
			Label(
				labels.OperatorSBR,
				labels.DisruptionNonDestructive,
				labels.TierAcceptance,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyNightly,
			), func() {
				Expect(nodeWatchdogDevices).ToNot(BeEmpty(),
					"watchdog inventory is empty — no schedulable nodes were checked")

				var hwNodes, noWatchdogNodes []string

				for nodeName, devs := range nodeWatchdogDevices {
					switch {
					case devs == nil:
						// Probe pod failed during BeforeAll; reported there as a warning.
						// Don't run softdog check — we can't distinguish "no hw watchdog" from "probe error".
						GinkgoWriter.Printf("Skipping node %s: watchdog probe failed during setup\n", nodeName)
					case len(devs) > 0:
						hwNodes = append(hwNodes, nodeName)
					default:
						noWatchdogNodes = append(noWatchdogNodes, nodeName)
					}
				}

				if len(hwNodes) == 0 && len(noWatchdogNodes) == 0 {
					Fail("watchdog integration test did not successfully inventory any schedulable nodes; " +
						"all probe pods likely failed during setup")
				}

				var errorMessages []string

				// Part 1: nodes with hardware watchdog — verify each device is a character device.
				By(fmt.Sprintf("Part 1: verifying hardware watchdog devices on %d node(s)", len(hwNodes)))

				for _, nodeName := range hwNodes {
					devices := nodeWatchdogDevices[nodeName]

					By(fmt.Sprintf("Checking hardware watchdog on node %s: %v", nodeName, devices))

					podName := watchdogDebugPodName("hwchk-" + nodeName)

					debugPod, createErr := pod.NewBuilder(
						APIClient, podName, medik8sparams.OperatorNs, sbrparams.WatchdogDebugImage).
						DefineOnNode(nodeName).
						WithHostPid(true).
						WithPrivilegedFlag().
						CreateAndWaitUntilRunning(medik8sparams.DefaultTimeout)
					if createErr != nil {
						errorMessages = append(errorMessages,
							fmt.Sprintf("node %s: failed to create debug pod: %v", nodeName, createErr))

						continue
					}

					DeferCleanup(func(name string) {
						if existing, pullErr := pod.Pull(APIClient, name, medik8sparams.OperatorNs); pullErr == nil {
							if _, delErr := existing.Delete(); delErr != nil {
								GinkgoWriter.Printf("Warning: failed to delete debug pod %s: %v\n", name, delErr)
							}
						}
					}, podName)

					for _, device := range devices {
						hostPath := "/proc/1/root" + device

						_, execErr := debugPod.ExecCommand([]string{"test", "-c", hostPath})
						if execErr != nil {
							errorMessages = append(errorMessages,
								fmt.Sprintf("node %s: watchdog device %s is not a character device or check failed: %v",
									nodeName, device, execErr))
						}
					}

					if _, delErr := debugPod.Delete(); delErr != nil {
						GinkgoWriter.Printf("Warning: failed to delete debug pod %s: %v\n", podName, delErr)
					}
				}

				// Part 2: nodes without hardware watchdog — verify softdog module is present in the
				// kernel so the SBR agent can load it as a fallback (/dev/watchdog backup path).
				// On RHCOS, modules live under /usr/lib/modules (not /lib/modules which is a symlink).
				By(fmt.Sprintf("Part 2: verifying softdog fallback on %d node(s) with no hardware watchdog",
					len(noWatchdogNodes)))

				for _, nodeName := range noWatchdogNodes {
					By(fmt.Sprintf("Checking softdog availability on node %s (no hardware watchdog found)", nodeName))

					podName := watchdogDebugPodName("softdog-" + nodeName)

					debugPod, createErr := pod.NewBuilder(
						APIClient, podName, medik8sparams.OperatorNs, sbrparams.WatchdogDebugImage).
						DefineOnNode(nodeName).
						WithHostPid(true).
						WithPrivilegedFlag().
						CreateAndWaitUntilRunning(medik8sparams.DefaultTimeout)
					if createErr != nil {
						errorMessages = append(errorMessages,
							fmt.Sprintf("node %s: failed to create softdog check pod: %v", nodeName, createErr))

						continue
					}

					DeferCleanup(func(name string) {
						if existing, pullErr := pod.Pull(APIClient, name, medik8sparams.OperatorNs); pullErr == nil {
							if _, delErr := existing.Delete(); delErr != nil {
								GinkgoWriter.Printf("Warning: failed to delete debug pod %s: %v\n", name, delErr)
							}
						}
					}, podName)

					// Three-step check (first match wins, always exits 0):
					// 1. A watchdog device exists already (softdog or hw not in inventory).
					// 2. softdog.ko* is present under /usr/lib/modules (RHCOS standard location).
					// 3. softdog.ko* is present under /lib/modules (fallback for other layouts).
					buf, execErr := debugPod.ExecCommand([]string{
						"sh", "-c",
						"ls /proc/1/root/dev/watchdog* 2>/dev/null | head -1 | grep -q . && echo loaded && exit 0;" +
							"find /proc/1/root/usr/lib/modules -name 'softdog.ko*' 2>/dev/null | head -1 | grep -q . " +
							"&& echo available && exit 0;" +
							"find /proc/1/root/lib/modules -name 'softdog.ko*' 2>/dev/null | head -1 | grep -q . " +
							"&& echo available && exit 0;" +
							"echo missing",
					})

					if _, delErr := debugPod.Delete(); delErr != nil {
						GinkgoWriter.Printf("Warning: failed to delete softdog check pod %s: %v\n", podName, delErr)
					}

					if execErr != nil {
						errorMessages = append(errorMessages,
							fmt.Sprintf("node %s: failed to check softdog status: %v", nodeName, execErr))

						continue
					}

					switch result := strings.TrimSpace(buf.String()); result {
					case "loaded":
						GinkgoWriter.Printf("  node %s: watchdog device already present\n", nodeName)
					case "available":
						GinkgoWriter.Printf("  node %s: softdog.ko found in kernel module tree\n", nodeName)
					default:
						errorMessages = append(errorMessages,
							fmt.Sprintf("node %s: no hardware watchdog and softdog module not found in kernel (got=%q)",
								nodeName, result))
					}
				}

				if len(errorMessages) > 0 {
					errMsg := "Watchdog integration check failures:\n"
					for _, msg := range errorMessages {
						errMsg += fmt.Sprintf("- %s\n", msg)
					}

					Fail(errMsg)
				}
			})
	})
