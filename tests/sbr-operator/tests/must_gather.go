package tests

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oplmV1alpha1 "github.com/rh-ecosystem-edge/eco-goinfra/pkg/schemes/olm/operators/v1alpha1"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
	"github.com/medik8s/system-tests/tests/sbr-operator/internal/sbrparams"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe(
	"SBR Must-Gather Diagnostics",
	Ordered,
	Label(labels.OperatorSBR), func() {
		It("Verify SBR must-gather collects diagnostic data",
			reportxml.ID("88733"),
			Label(
				labels.DisruptionNonDestructive,
				labels.TierAcceptance,
				labels.PlatformAny,
				labels.ComponentController,
				labels.FrequencyWeekly,
			), func() {
				By("Verifying SBR deployment is Ready")

				sbrDeployment, err := deployment.Pull(
					APIClient, sbrparams.OperatorDeploymentName, medik8sparams.OperatorNs)
				Expect(err).ToNot(HaveOccurred(), "Failed to get SBR deployment")
				Expect(sbrDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
					"SBR deployment is not Ready")

				By("Resolving the RHWA must-gather image")

				mustGatherImage := resolveMustGatherImage()
				Expect(mustGatherImage).To(ContainSubstring(":"),
					"must-gather image %q should contain a tag separator", mustGatherImage)
				GinkgoWriter.Printf("Using must-gather image: %s\n", mustGatherImage)

				By("Creating artifact directory for must-gather output")

				destDir := createMustGatherDestDir()

				By("Capturing cluster state before must-gather for validation")

				nodeList, err := APIClient.CoreV1Interface.Nodes().List(context.TODO(), metav1.ListOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to list cluster nodes")
				Expect(nodeList.Items).ToNot(BeEmpty(), "Cluster has no nodes")

				var nodeNames []string
				for i := range nodeList.Items {
					nodeNames = append(nodeNames, nodeList.Items[i].Name)
				}

				By("Running oc adm must-gather")

				testStartTime := time.Now()

				ctx, cancel := context.WithTimeout(context.Background(), sbrparams.MustGatherContextTimeout)
				defer cancel()

				DeferCleanup(func() {
					cleanupMustGatherNamespaces(context.Background(), testStartTime)
				})

				runMustGather(ctx, mustGatherImage, destDir)

				By("Collecting gathered file paths")

				collectedFiles, walkErr := collectRelativePaths(destDir)
				Expect(walkErr).ToNot(HaveOccurred(), "Failed to walk must-gather output directory")
				Expect(collectedFiles).ToNot(BeEmpty(), "No files collected by must-gather")

				if writeErr := os.WriteFile(filepath.Join(destDir, "collected-paths.txt"),
					[]byte(strings.Join(collectedFiles, "\n")+"\n"), 0o644); writeErr != nil {
					GinkgoWriter.Printf("Warning: failed to write collected-paths.txt: %v\n", writeErr)
				}

				By("Validating node YAMLs for all cluster nodes")

				for _, nodeName := range nodeNames {
					Expect(hasMatchingFile(collectedFiles, "/nodes/"+nodeName+".yaml")).To(BeTrue(),
						"must-gather should contain YAML for node %s", nodeName)
				}

				By("Validating SBR CRD definitions are present")

				for _, crdName := range sbrparams.SBRCRDNames {
					Expect(hasMatchingFile(collectedFiles, crdName+".yaml")).To(BeTrue(),
						"must-gather should contain CRD definition for %s", crdName)
				}

				By("Validating MachineHealthCheck data is collected")

				Expect(hasMatchingFile(collectedFiles, "machinehealthchecks")).To(BeTrue(),
					"must-gather should contain MachineHealthCheck data")
			})
	})

func resolveMustGatherImage() string {
	if envImg := os.Getenv("MUST_GATHER_IMAGE"); envImg != "" {
		GinkgoWriter.Printf("must-gather image resolved from MUST_GATHER_IMAGE env var\n")

		return envImg
	}

	nhcCSVs, err := olm.ListClusterServiceVersionWithNamePattern(
		APIClient, "node-healthcheck", medik8sparams.OperatorNs)
	if err != nil {
		GinkgoWriter.Printf("Warning: failed to list NHC CSVs for must-gather image resolution: %v\n", err)
	} else {
		for _, csv := range nhcCSVs {
			phase, phaseErr := csv.GetPhase()
			if phaseErr != nil || phase != oplmV1alpha1.CSVPhaseSucceeded {
				continue
			}

			version := csv.Object.Spec.Version.String()
			if version != "" {
				image := fmt.Sprintf("%s:v%s", sbrparams.MustGatherImageRepo, version)
				GinkgoWriter.Printf("must-gather image resolved from NHC CSV version: %s\n", image)

				return image
			}
		}
	}

	GinkgoWriter.Printf("WARNING: must-gather image using hardcoded fallback tag %s\n",
		sbrparams.MustGatherDefaultTag)

	return fmt.Sprintf("%s:%s", sbrparams.MustGatherImageRepo, sbrparams.MustGatherDefaultTag)
}

func createMustGatherDestDir() string {
	base := os.Getenv("ARTIFACT_DIR")
	if base == "" {
		base = GinkgoT().TempDir()
	}

	dir, mkdirErr := os.MkdirTemp(base, "sbr-must-gather-")
	ExpectWithOffset(1, mkdirErr).ToNot(HaveOccurred(), "Failed to create must-gather output directory")

	return dir
}

func runMustGather(ctx context.Context, image, destDir string) {
	ocTimeout := fmt.Sprintf("%ds", int(sbrparams.MustGatherOCTimeout.Seconds()))

	cmd := exec.CommandContext(ctx, "oc", "adm", "must-gather",
		"--image="+image,
		"--dest-dir="+destDir,
		"--timeout="+ocTimeout,
	)

	env := os.Environ()
	if os.Getenv("HOME") == "" {
		env = append(env, "HOME=/tmp")
	}

	cmd.Env = env

	output, err := cmd.CombinedOutput()

	logFile := filepath.Join(destDir, "oc-adm-must-gather.log")

	if writeErr := os.WriteFile(logFile, output, 0o644); writeErr != nil {
		GinkgoWriter.Printf("Warning: failed to write must-gather log to %s: %v\n", logFile, writeErr)
	}

	GinkgoWriter.Printf("must-gather output saved to %s\n", logFile)

	if ctx.Err() != nil {
		Fail(fmt.Sprintf("must-gather timed out after %s:\n%s",
			sbrparams.MustGatherContextTimeout, string(output)))
	}

	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "must-gather failed:\n%s", string(output))
}

func collectRelativePaths(root string) ([]string, error) {
	var paths []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		paths = append(paths, filepath.ToSlash(rel))

		return nil
	})

	return paths, err
}

func hasMatchingFile(files []string, pattern string) bool {
	lowerPattern := strings.ToLower(pattern)
	for _, f := range files {
		if strings.Contains(strings.ToLower(f), lowerPattern) {
			return true
		}
	}

	return false
}

func cleanupMustGatherNamespaces(ctx context.Context, testStartTime time.Time) {
	cleanupCtx, cleanupCancel := context.WithTimeout(ctx, sbrparams.MustGatherCleanupTimeout)
	defer cleanupCancel()

	out, err := exec.CommandContext(cleanupCtx, "oc", "get", "ns",
		"-l", "openshift.io/run-level",
		"-o", "jsonpath={range .items[*]}{.metadata.name} {.metadata.creationTimestamp}{\"\\n\"}{end}",
	).CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("Warning: failed to list namespaces for must-gather cleanup: %v\n", err)

		return
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 1 || !strings.HasPrefix(fields[0], "openshift-must-gather-") {
			continue
		}

		namespaceName := fields[0]

		if len(fields) >= 2 {
			createdAt, parseErr := time.Parse(time.RFC3339, fields[1])
			if parseErr == nil && createdAt.Before(testStartTime) {
				continue
			}
		}

		GinkgoWriter.Printf("Cleaning up leftover must-gather namespace: %s\n", namespaceName)

		cleanupOut, cleanupErr := exec.CommandContext(cleanupCtx, "oc", "delete", "ns", namespaceName,
			"--ignore-not-found", "--wait=false").CombinedOutput()
		if cleanupErr != nil {
			GinkgoWriter.Printf("Warning: failed to delete namespace %s: %v\n%s\n",
				namespaceName, cleanupErr, string(cleanupOut))
		}
	}
}
