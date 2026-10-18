package tests

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/internal/mustgather"
	"github.com/medik8s/system-tests/tests/mdr-operator/internal/mdrparams"
)

// nodesCollectionPath is where oc adm inspect writes cluster-scoped node YAMLs.
const nodesCollectionPath = "cluster-scoped-resources/core/nodes"

var _ = Describe("MDR Observability Tests",
	Serial,
	Ordered,
	ContinueOnFailure,
	Label(labels.OperatorMDR, mdrparams.Label,
		labels.TierAcceptance, labels.FrequencyWeekly),
	func() {
		var ctx context.Context

		BeforeAll(func() {
			ctx = context.Background()

			By("Verifying MDR operator deployment is ready")

			mdrDeployment, err := deployment.Pull(
				APIClient, mdrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get MDR deployment")
			Expect(mdrDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"MDR deployment is not Ready")

			By("Pre-cleaning stale must-gather test resources from previous runs")

			cleanupMDRCR(mdrparams.MDRMustGatherTestName)
			cleanupMDRT(mdrparams.MDRTMustGatherTestName)
		})

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				logMDRControllerState()
			}
		})

		AfterAll(func() {
			// Ginkgo runs AfterAll before the DeferCleanup callbacks registered inside
			// the It, so the fixtures are deleted here -- otherwise the controller check
			// below would run while they still exist. Both helpers tolerate NotFound,
			// so this is safe alongside the DeferCleanup registrations.
			By("Removing must-gather test resources")

			cleanupMDRCR(mdrparams.MDRMustGatherTestName)
			cleanupMDRT(mdrparams.MDRTMustGatherTestName)

			By("Verifying MDR controller pod is running")

			Eventually(verifyMDRControllerRunning,
				medik8sparams.DefaultTimeout, mdrparams.DefaultPollInterval).Should(Succeed(),
				"MDR controller pod should be running after the must-gather test")
		})

		Context("must-gather", func() {
			It("collects MDR data via oc adm must-gather",
				Label(labels.DisruptionNonDestructive, labels.PlatformAny,
					labels.ComponentController),
				reportxml.ID("62581"),
				func() {
					By("Verifying the must-gather MDR CR name does not match a cluster node")

					// MDR targets the node named after the remediation CR, so a name
					// collision here would delete a real Machine.
					nodeObj := &corev1.Node{}
					nodeErr := APIClient.Get(ctx,
						client.ObjectKey{Name: mdrparams.MDRMustGatherTestName}, nodeObj)
					Expect(k8serrors.IsNotFound(nodeErr)).To(BeTrue(),
						"node %q must be absent before creating an MDR CR with that name; "+
							"Get returned: %v", mdrparams.MDRMustGatherTestName, nodeErr)

					By("Creating temporary must-gather output directory")

					mustGatherDir, mkdirErr := os.MkdirTemp("", "mdr-must-gather-*")
					Expect(mkdirErr).ToNot(HaveOccurred(), "Failed to create must-gather output directory")

					DeferCleanup(func() {
						// On failure the archive is the only evidence of what the
						// collector actually produced, so keep it for triage.
						if CurrentSpecReport().Failed() {
							GinkgoWriter.Printf("Spec failed -- preserving must-gather output at %s\n",
								mustGatherDir)

							return
						}

						if removeErr := os.RemoveAll(mustGatherDir); removeErr != nil {
							GinkgoWriter.Printf("WARNING: failed to remove must-gather dir %s: %v\n",
								mustGatherDir, removeErr)
						}
					})

					By("Creating the MDR template CR for must-gather to collect")

					// Cleanup is registered before the create so a create that persists
					// server-side but fails client-side is still torn down.
					DeferCleanup(func() { cleanupMDRT(mdrparams.MDRTMustGatherTestName) })
					Expect(APIClient.Create(ctx, buildMDRT(mdrparams.MDRTMustGatherTestName))).To(Succeed(),
						"Failed to create MDRT %s", mdrparams.MDRTMustGatherTestName)

					By("Creating the MDR remediation CR for must-gather to collect")

					deferDeleteMDRCR(mdrparams.MDRMustGatherTestName)
					Expect(APIClient.Create(ctx, buildMDR(mdrparams.MDRMustGatherTestName))).To(Succeed(),
						"Failed to create MDR %s", mdrparams.MDRMustGatherTestName)

					By("Capturing cluster state to validate the collection against")

					nodeNames := listNodeNames(ctx)
					podsBeforeRun := runningControllerPodNames()
					GinkgoWriter.Printf("Validating against %d node(s); controller pod(s) before run: %v\n",
						len(nodeNames), podsBeforeRun)

					By("Running oc adm must-gather")

					mustGatherImage := os.Getenv(mdrparams.MustGatherImageEnvVar)
					if mustGatherImage == "" {
						mustGatherImage = mdrparams.DefaultMustGatherImage
					}

					collectionStart := time.Now()

					// oc normally deletes its own collection namespace; this is the net
					// for the case where it is killed before that runs.
					DeferCleanup(func() {
						mustgather.CleanupNamespaces(context.Background(), collectionStart,
							mdrparams.MustGatherCleanupTimeout, GinkgoWriter.Printf)
					})

					Expect(mustgather.Run(ctx, mustGatherImage, mustGatherDir, mustgather.Options{
						CommandTimeout:   mdrparams.MustGatherTimeout,
						OCTimeout:        mdrparams.MustGatherOCTimeout,
						ImageInfoTimeout: mdrparams.MustGatherImageInfoTimeout,
						SaveCommandLog:   true,
					}, GinkgoWriter.Printf)).To(Succeed(), "oc adm must-gather failed")

					By("Validating must-gather output contains MDR data")

					missingItems := mustgather.ValidateMustGatherContents(
						mustGatherDir, buildMDRMustGatherExpectations(nodeNames))
					if len(missingItems) > 0 {
						logCollectedPaths(mustGatherDir)
					}

					Expect(missingItems).To(BeEmpty(),
						"Must-gather validation failed:\n%s", strings.Join(missingItems, "\n"))

					By("Validating the MDR controller pod YAML and non-empty manager logs were collected")

					// Re-resolve after the run: a pod replaced mid-collection would
					// otherwise fail a correct archive.
					verifyControllerPodArtifacts(mustGatherDir,
						mergeNames(podsBeforeRun, runningControllerPodNames()))
				})
		})
	})

// listNodeNames returns the names of every node in the cluster, sorted so the
// logged output is stable.
func listNodeNames(ctx context.Context) []string {
	GinkgoHelper()
	nodeList := &corev1.NodeList{}
	Expect(APIClient.List(ctx, nodeList)).To(Succeed(), "Failed to list cluster nodes")
	Expect(nodeList.Items).ToNot(BeEmpty(), "Cluster has no nodes")
	names := make([]string, 0, len(nodeList.Items))
	for i := range nodeList.Items {
		names = append(names, nodeList.Items[i].Name)
	}

	sort.Strings(names)

	return names
}

// runningControllerPodNames returns the names of the Running, ready MDR
// controller pods. Terminating and not-yet-ready pods are excluded: must-gather
// collects no logs for them, which would fail the archive checks for a reason
// unrelated to collection.
func runningControllerPodNames() []string {
	GinkgoHelper()
	mdrPods, listErr := pod.List(APIClient, medik8sparams.OperatorNs, metav1.ListOptions{
		LabelSelector: mdrparams.OperatorControllerPodLabelSelector,
	})
	Expect(listErr).ToNot(HaveOccurred(), "Failed to list MDR controller pods")
	controllerPods := helpers.FilterRunningPods(
		helpers.FilterPodsByDeployment(mdrPods, mdrparams.OperatorDeploymentName))
	Expect(controllerPods).ToNot(BeEmpty(),
		"No Running MDR controller pods found in %s", medik8sparams.OperatorNs)
	names := make([]string, 0, len(controllerPods))
	for _, controllerPod := range controllerPods {
		names = append(names, controllerPod.Object.Name)
	}

	sort.Strings(names)

	return names
}

// mergeNames returns the sorted union of two name slices.
func mergeNames(first, second []string) []string {
	seen := make(map[string]struct{}, len(first)+len(second))

	for _, name := range append(append([]string{}, first...), second...) {
		seen[name] = struct{}{}
	}

	merged := make([]string, 0, len(seen))
	for name := range seen {
		merged = append(merged, name)
	}

	sort.Strings(merged)

	return merged
}

// verifyControllerPodArtifacts asserts the archive holds the controller pod's
// own YAML and a non-empty manager container log. Both checks are done here
// rather than as MustGatherExpectations: a path-only expectation is satisfied by
// any file under the pod directory, and the validator counts filenames without
// looking at size, so an empty log file would pass.
func verifyControllerPodArtifacts(baseDir string, podNames []string) {
	GinkgoHelper()
	paths, walkErr := mustgather.CollectRelativePaths(baseDir)
	Expect(walkErr).ToNot(HaveOccurred(), "Failed to walk must-gather output directory")

	var podYAMLFound, managerLogFound bool

	for _, podName := range podNames {
		for _, relPath := range paths {
			if !strings.Contains(relPath, podName) {
				continue
			}

			base := filepath.Base(relPath)
			if base == podName+".yaml" {
				podYAMLFound = true
			}

			if !strings.HasSuffix(base, ".log") ||
				!strings.Contains(relPath, mdrparams.ManagerContainerName) {
				continue
			}

			if info, statErr := os.Stat(filepath.Join(baseDir, relPath)); statErr == nil && info.Size() > 0 {
				managerLogFound = true
			}
		}
	}

	if !podYAMLFound || !managerLogFound {
		logCollectedPaths(baseDir)
	}

	Expect(podYAMLFound).To(BeTrue(),
		"must-gather should contain <pod>.yaml for one of the MDR controller pods %v", podNames)
	Expect(managerLogFound).To(BeTrue(),
		"must-gather should contain a non-empty %q container log for one of the MDR controller pods %v",
		mdrparams.ManagerContainerName, podNames)
}

// logCollectedPaths dumps the archive layout so a validation failure can be
// diagnosed from the CI log alone.
func logCollectedPaths(baseDir string) {
	paths, walkErr := mustgather.CollectRelativePaths(baseDir)
	if walkErr != nil {
		GinkgoWriter.Printf("Could not list must-gather output at %s: %v\n", baseDir, walkErr)

		return
	}

	GinkgoWriter.Printf("must-gather collected %d path(s) under %s:\n%s\n",
		len(paths), baseDir, strings.Join(paths, "\n"))
}

// buildMDRMustGatherExpectations lists the archive artifacts OCP-62581 requires.
// The controller pod YAML and logs are asserted separately, in
// verifyControllerPodArtifacts.
func buildMDRMustGatherExpectations(nodeNames []string) []mustgather.MustGatherExpectation {
	mdrResourcePath := mdrparams.CRDGroup + "/machinedeletionremediations"
	mdrtResourcePath := mdrparams.CRDGroup + "/machinedeletionremediationtemplates"
	expectations := make([]mustgather.MustGatherExpectation, 0, len(nodeNames)+len(mdrparams.MDRCRDNames)+3)

	for _, nodeName := range nodeNames {
		expectations = append(expectations, mustgather.MustGatherExpectation{
			Description:  "node YAML for " + nodeName,
			PathContains: nodesCollectionPath,
			NameGlob:     nodeName + ".yaml",
			MinCount:     1,
		})
	}

	for _, crdName := range mdrparams.MDRCRDNames {
		expectations = append(expectations, mustgather.MustGatherExpectation{
			Description: "MDR CRD definition " + crdName,
			NameGlob:    crdName + ".yaml",
			MinCount:    1,
		})
	}

	return append(expectations,
		mustgather.MustGatherExpectation{
			Description:  "MDR template CR " + mdrparams.MDRTMustGatherTestName,
			PathContains: mdrtResourcePath,
			NameGlob:     mdrparams.MDRTMustGatherTestName + ".yaml",
			MinCount:     1,
		},
		mustgather.MustGatherExpectation{
			Description:  "MDR remediation CR " + mdrparams.MDRMustGatherTestName,
			PathContains: mdrResourcePath,
			NameGlob:     mdrparams.MDRMustGatherTestName + ".yaml",
			MinCount:     1,
		},
		mustgather.MustGatherExpectation{
			Description:  "Machine API namespace contents",
			PathContains: "namespaces/" + mdrparams.MachineAPINamespace + "/",
			NameGlob:     "*.yaml",
			MinCount:     1,
		},
	)
}
