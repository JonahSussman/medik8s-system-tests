# SNR Operator Post-Deployment Tests

Automated tests validating the Self Node Remediation (SNR) operator
deployment, configuration, and OLM metadata.

## Prerequisites

- OpenShift cluster with SNR operator installed via OLM
- `KUBECONFIG` set with cluster-admin access
- SNR installed in `openshift-workload-availability` namespace

## Running

```bash
ginkgo --label-filter="snr" ./tests/snr-operator/...
```

Or via the test runner:

```bash
export KUBECONFIG=/path/to/kubeconfig
export ECO_TEST_FEATURES="snr-operator"
make run-tests
```

## Tests

### 1. Verify SNR Resources Are Installed and Running (Polarion OCP-54205)

Validates that the SelfNodeRemediationConfig CR exists, DaemonSet pods
are running, and controller-manager pods are in Running state.

- **Operators**: SNR v0.13.0+
- **Cluster**: Any topology (MNO or SNO)
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="snr" --focus="resources are installed" ./tests/snr-operator/...`
- **Pass criteria**: SNRC exists by name, at least one DS pod running, all controller pods running

### 2. Verify Only Automatic Remediation Template Exists (Polarion OCP-71010)

Validates that the "Automatic" SelfNodeRemediationTemplate exists with
the correct remediation strategy, and that unsupported templates
(ResourceDeletion, NodeDeletion) do not exist.

- **Operators**: SNR v0.13.0+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="snr" --focus="Automatic remediation template" ./tests/snr-operator/...`
- **Pass criteria**: Automatic template exists with strategy "Automatic", ResourceDeletion and NodeDeletion templates return NotFound

### 3. Verify SNR CSV Annotations (Polarion OCP-52136)

Validates that the active SNR ClusterServiceVersion (in Succeeded phase)
has required OLM annotations: valid-subscription, support contact,
repository URL, and at least one maintainer.

- **Operators**: SNR v0.13.0+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="snr" --focus="CSV annotations" ./tests/snr-operator/...`
- **Pass criteria**: All required annotations present, maintainers list non-empty

### 4. Verify SNR CSV Metadata (Polarion OCP-70705)

Validates infrastructure feature annotations (disconnected, fips-compliant,
proxy-aware, etc.), suggested-namespace points to
openshift-workload-availability, replaces field references previous SNR
version, and controller replicas match expected count on multi-node clusters.

- **Operators**: SNR v0.13.0+
- **Cluster**: Multi-node for replica check (skips replica validation on SNO)
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="snr" --focus="CSV metadata" ./tests/snr-operator/...`
- **Pass criteria**: All infrastructure annotations match expected values, suggested-namespace correct, replaces field contains "self-node-remediation", 2 ready replicas on MNO
