package medik8sparams

import (
	"os"
	"time"
)

const (
	// Label represents medik8s label that can be used for test cases selection.
	Label = "medik8s"
	// OperatorNs custom namespace of medik8s operators.
	OperatorNs = "openshift-workload-availability"
	// DefaultTimeout represents the default timeout.
	DefaultTimeout = 300 * time.Second
	// DevelopmentCSVVersion is the default VERSION from Medik8s operator Makefiles on main
	// (IMAGE_TAG=latest). Product / release CSVs use real semver (e.g. 0.3.0).
	DevelopmentCSVVersion = "0.0.1"
)

// WorkloadImage is the container image used for test workload pods.
// Set via WORKLOAD_IMAGE env var. In Prow CI this is written to
// SHARED_DIR/workload_image by the medik8s-lib step and exported by the
// e2e-test commands block. For local runs set it manually:
//
//	export WORKLOAD_IMAGE=registry.access.redhat.com/ubi9/ubi-minimal:latest
var WorkloadImage = func() string {
	img := os.Getenv("WORKLOAD_IMAGE")
	if img == "" {
		panic("WORKLOAD_IMAGE env var is required but not set. " +
			"In Prow CI this is exported from SHARED_DIR/workload_image by the e2e-test commands block. " +
			"For local runs: export WORKLOAD_IMAGE=registry.access.redhat.com/ubi9/ubi-minimal:latest")
	}

	return img
}()
