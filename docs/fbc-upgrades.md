# File-Based Catalog Operator Upgrades

The shared `tier:upgrade-operator` runner handles the catalog and OLM transition
for NHC, SBR, SNR, FAR, MDR, and NMO. Operator-specific lifecycle hooks supply
the configuration and behavior checks:

1. Apply the release-provided ImageDigestMirrorSet and wait for any
   MachineConfigPool rollout.
2. Create a test-owned CatalogSource from a digest-pinned FBC image.
3. Install the released operator from `redhat-operators`.
4. Exercise operator-specific behavior and capture persistent state.
5. Switch the existing Subscription to the candidate FBC.
6. Require a new Succeeded CSV with the exact candidate version.
7. Require the live controller to run the expected digest-pinned image.
8. Verify preserved state, fresh reconciliation, and operator-specific behavior.

## Validation coverage

NHC's validations moved from `upgrade_operator.go` into
`tests/nhc-operator/tests/upgrade_lifecycle.go`; they are not deferred to a stub.
`BeforeUpgrade` creates the configuration, captures its UID and complete spec,
and exercises baseline reconciliation and remediation. `AfterUpgrade` compares
the UID and complete spec, requires a fresh pause response, restores the original
configuration, and exercises candidate remediation. The shared runner calls both
hooks around the Subscription upgrade and requires them to succeed.

SBR also has concrete hooks in `tests/sbr-operator/tests/upgrade_lifecycle.go`.
They capture and compare the config UID and complete spec, prove fresh
reconciliation, and restore the original configuration. Its default no-storage
mode and optional remediation check are described below.

SNR has concrete hooks in `tests/snr-operator/tests/upgrade_lifecycle.go`.
They customize the GA-created default config, compare its UID and complete spec
after upgrading, and require a unique config probe to reach newly rolled agent
pods. Both the controller and all agents must run the expected candidate image.
A direct SNR remediation must reboot and recover a worker before and after the
upgrade; boot IDs must change while the node UID remains the same. This scenario
is destructive and requires at least two Ready workers on a disposable cluster.
It does not install NHC or stop kubelet: a test-owned SNR CR triggers remediation.

FAR, MDR, and NMO still use `NewStubUpgradeOperatorTest`.
Their stubs provide only the shared catalog/CSV/version/image checks,
not configuration persistence or operator-specific behavior validation.
Follow-ups replace each operator's factory with concrete lifecycle hooks;
operator-specific validations do not belong in the generic `stub.go`.

## Inputs

Each suite uses variables prefixed by its operator name. For NHC:

```bash
export NHC_FBC_CATALOG_IMAGE='quay.io/example/rhwa-fbc@sha256:...'
# Required only when the catalog relies on registry mirrors:
export NHC_FBC_IDMS_PATH=/absolute/path/to/idms.yaml
export NHC_FBC_CANDIDATE_VERSION=5.8.0
export NHC_FBC_CANDIDATE_IMAGE='registry.example/operator@sha256:...'

# Optional defaults:
export NHC_FBC_CATALOG_NAME=nhc-upgrade-candidate
export NHC_FBC_CHANNEL=stable
export NHC_FBC_SKIP_CLEANUP=false
```

Use the corresponding `SBR_FBC_*`, `SNR_FBC_*`, `FAR_FBC_*`, `MDR_FBC_*`, or
`NMO_FBC_*` variables for another operator. This lets multiple suites use
different catalogs, versions, and images in the same test process.

When `<OPERATOR>_FBC_IDMS_PATH` is unset, the test uses `$SHARED_DIR/idms.yaml` if
that file exists. Otherwise it skips IDMS application, which is appropriate for
directly pullable source-built catalogs. The catalog and candidate images must
use immutable `@sha256:` pullspecs. Registry credentials must already be
present in the cluster pull secret; tests never acquire or print credentials.

## Run one operator

```bash
export KUBECONFIG=/absolute/path/to/kubeconfig
export ECO_TEST_FEATURES=nhc-operator
export ECO_TEST_LABELS='tier:upgrade-operator'
export WORKLOAD_IMAGE=registry.access.redhat.com/ubi9/ubi-minimal:latest
export ECO_REPORTS_DUMP_DIR="$(mktemp -d)"
make run-tests
```

Replace `nhc-operator` with `sbr-operator`, `snr-operator`, `far-operator`,
`mdr-operator`, or `nmo-operator`. NHC, SBR, and SNR have operator-specific lifecycle
checks; FAR, MDR, and NMO currently have lifecycle stubs.

## SBR: upgrade and configuration without storage

By default, SBR needs no ODF installation or RWX StorageClass. Its test installs
the GA baseline, creates a safe `StorageBasedRemediationConfig` without shared
storage, switches the Subscription to the candidate FBC, and requires:

- A different Succeeded CSV with the exact candidate version and controller image.
- The same config UID and complete spec, rather than a recreated config.
- A fresh candidate-controller response to a unique, intentionally nonexistent
  StorageClass, followed by restoration of the original config spec.
- No PVC or agent DaemonSet from this no-storage probe.

SBR intentionally reports `PVCError` when storage is absent. The test uses that
expected validation response as evidence of reconciliation; it does **not**
claim that agents are Ready or that functional remediation works without storage.
Logs and reports explicitly say `remediation NOT REQUESTED` in the default mode.
Both source-built and downstream FBCs use these same checks and `SBR_FBC_*` inputs.
No `operator-sdk run bundle-upgrade` is involved.

To additionally exercise Emily's real reboot-and-recovery check from PR #11:

```bash
export SBR_FBC_REMEDIATION=true
export SBR_STORAGE_CLASS=existing-rwx-storage-class
```

This opt-in requires existing RWX storage, at least two healthy worker agents,
and a target worker not hosting an SBR controller. It creates an owned,
storage-backed config, triggers SBR, and requires a changed boot ID and a Ready
node afterward. It never installs ODF or another storage provider. The suite
is labelled `disruption:destructive` only when this mode is enabled; otherwise
it is `disruption:nondestructive`. Missing opt-in prerequisites fail rather than
silently skipping the upgrade test. `SBR_STORAGE_CLASS` alone does not enable
storage or remediation.

The tests are designed for disposable clusters. Candidate CatalogSource and
OLM cleanup failures are logged as warnings. The applied IDMS remains in place
to avoid triggering a second MachineConfigPool rollout during teardown.
