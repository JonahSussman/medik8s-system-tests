# File-Based Catalog Operator Upgrades

The `tier:upgrade-operator-fbc` tests exercise the same release contract for
NHC, SBR, SNR, FAR, MDR, and NMO:

1. Apply the release-provided ImageDigestMirrorSet and wait for any
   MachineConfigPool rollout.
2. Create a test-owned CatalogSource from a digest-pinned FBC image.
3. Install the released operator from `redhat-operators`.
4. Exercise operator-specific behavior and capture persistent state.
5. Switch the existing Subscription to the candidate FBC.
6. Require a new Succeeded CSV with the exact candidate version.
7. Require the live controller and operands to run the expected digest-pinned
   image.
8. Verify preserved state, fresh reconciliation, and operator-specific behavior.

## Inputs

All six suites use the same environment variables:

```bash
export MEDIK8S_FBC_CATALOG_IMAGE='quay.io/example/rhwa-fbc@sha256:...'
export MEDIK8S_FBC_IDMS_PATH=/absolute/path/to/idms.yaml
export MEDIK8S_FBC_CANDIDATE_VERSION=5.8.0
export MEDIK8S_FBC_CANDIDATE_IMAGE='registry.example/operator@sha256:...'

# Optional defaults:
export MEDIK8S_FBC_CATALOG_NAME=medik8s-upgrade-candidate
export MEDIK8S_FBC_CHANNEL=stable
export MEDIK8S_FBC_SKIP_CLEANUP=false
```

When `MEDIK8S_FBC_IDMS_PATH` is unset, the test uses
`$SHARED_DIR/idms.yaml`. The catalog and candidate images must use immutable
`@sha256:` pullspecs. Registry credentials must already be present in the
cluster pull secret; tests never acquire or print credentials.

## Run one operator

```bash
export KUBECONFIG=/absolute/path/to/kubeconfig
export ECO_TEST_FEATURES=nhc-operator
export ECO_TEST_LABELS='tier:upgrade-operator-fbc'
export WORKLOAD_IMAGE=registry.access.redhat.com/ubi9/ubi-minimal:latest
export ECO_REPORTS_DUMP_DIR="$(mktemp -d)"
make run-tests
```

Replace `nhc-operator` with `sbr-operator`, `snr-operator`, `far-operator`,
`mdr-operator`, or `nmo-operator`. SBR also requires `SBR_STORAGE_CLASS` to
name an RWX-capable class. FAR requires AWS credentials available through the
cluster CredentialsRequest. The destructive suites require enough healthy
workers for safe remediation.

The tests are designed for disposable clusters. Candidate CatalogSource and
OLM cleanup failures are logged as warnings. The applied IDMS remains in place
to avoid triggering a second MachineConfigPool rollout during teardown.
