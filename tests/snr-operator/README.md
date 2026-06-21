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

### 5. Verify SNR with Unsupported NodeDeletion Strategy Is Rejected ([OCP-60877](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-60877))

Validates that creating a SelfNodeRemediation CR with the unsupported
NodeDeletion remediation strategy is rejected by CRD validation.

- **Operators**: SNR v0.12.1+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="snr" --focus="SNR with unsupported" ./tests/snr-operator/...`
- **Pass criteria**: API server rejects with "Unsupported value" and "NodeDeletion"

### 6. Verify SNRT with Unsupported NodeDeletion Strategy Is Rejected ([OCP-60822](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-60822))

Validates that creating a SelfNodeRemediationTemplate with the unsupported
NodeDeletion remediation strategy is rejected by CRD validation.

- **Operators**: SNR v0.12.1+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="snr" --focus="SNRT with unsupported" ./tests/snr-operator/...`
- **Pass criteria**: API server rejects with "Unsupported value" and "NodeDeletion"

### 7. Verify SNR Conditions with nhc-timed-out Annotation ([OCP-60881](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-60881))

Validates that creating a SelfNodeRemediation CR with the nhc-timed-out
annotation causes Processing and Succeeded conditions to reflect
RemediationTimeoutByNHC reason.

- **Operators**: SNR v0.12.1+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="snr" --focus="nhc-timed-out annotation" ./tests/snr-operator/...`
- **Pass criteria**: Processing=False/RemediationTimeoutByNHC, Succeeded=False/RemediationTimeoutByNHC

### 8. Verify SNR Conditions with Non-Existent Node Name ([OCP-70584](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-70584))

Validates that creating a SelfNodeRemediation CR with a non-existent node
name causes Processing and Succeeded conditions to reflect
RemediationSkippedNodeNotFound reason.

- **Operators**: SNR v0.12.1+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="snr" --focus="non-existent node name" ./tests/snr-operator/...`
- **Pass criteria**: Processing=False/RemediationSkippedNodeNotFound, Succeeded=False/RemediationSkippedNodeNotFound
