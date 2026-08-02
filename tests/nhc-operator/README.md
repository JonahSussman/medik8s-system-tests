# NHC Operator Post-Deployment Tests

Automated tests validating the Node Health Check (NHC) operator
deployment, OLM metadata, and security posture.

## Prerequisites

- OpenShift cluster with NHC operator installed via OLM
- `KUBECONFIG` set with cluster-admin access
- NHC installed in `openshift-workload-availability` namespace
- Minimum tested version: NHC v0.12.0 (RHWA 4.22 GA baseline)

## Running

```bash
ginkgo --label-filter="nhc" ./tests/nhc-operator/...
```

Or via the test runner:

```bash
export KUBECONFIG=/path/to/kubeconfig
export ECO_TEST_FEATURES="nhc-operator"
make run-tests
```

## Tests

### 1. Verify NHC Resources Are Installed and Running ([OCP-89629](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-89629))

Validates that the NodeHealthCheck CRD is registered and listable, and
that NHC controller-manager pods are in Running state with all
containers ready.

- **Operators**: NHC v0.12.0+
- **Cluster**: Any topology (MNO or SNO)
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nhc" --focus="resources are installed" ./tests/nhc-operator/...`
- **Pass criteria**: NodeHealthCheck API is listable; all controller-manager pods are Running with all containers ready

### 2. Verify NHC CSV Annotations ([OCP-89630](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-89630))

Validates that the active NHC ClusterServiceVersion (in Succeeded phase)
has required OLM annotations: valid-subscription, support contact,
repository URL, and at least one maintainer.

- **Operators**: NHC v0.12.0+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nhc" --focus="CSV annotations" ./tests/nhc-operator/...`
- **Pass criteria**: All required annotations present, maintainers list non-empty

### 3. Verify NHC CSV Metadata ([OCP-89631](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-89631))

Validates that infrastructure feature annotations (disconnected, fips-compliant,
proxy-aware, etc.) match expected values, the `replaces` field references
the previous NHC version when present, and controller replicas match the
expected count on multi-node clusters. Skips replica validation on SNO.

- **Operators**: NHC v0.12.0+
- **Cluster**: Multi-node for replica check (skips replica validation on SNO)
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nhc" --focus="CSV metadata" ./tests/nhc-operator/...`
- **Pass criteria**: All infrastructure annotations match expected values, suggested-namespace correct, replaces field contains "node-healthcheck-operator" when set, 2 ready replicas on MNO

### 4. Verify NHC Container Runs as Non-Root User ([OCP-89632](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-89632))

Validates that the NHC manager container enforces a restricted security
context: runAsNonRoot at pod level, runAsUser is not UID 0 when set,
allowPrivilegeEscalation=false, capabilities.drop=ALL,
readOnlyRootFilesystem=true, and seccompProfile=RuntimeDefault (at
container or pod level). Only checks the `manager` container.

- **Operators**: NHC v0.12.0+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nhc" --focus="runs as non-root" ./tests/nhc-operator/...`
- **Pass criteria**: Pod runAsNonRoot=true; expected manager container exists; manager container runAsUser != 0; allowPrivilegeEscalation=false; readOnlyRootFilesystem=true; capabilities.drop=[ALL]; seccomp profile RuntimeDefault

## Destructive Tests -- Remediation Trigger and CR Lifecycle

Tests that stop kubelet on worker nodes and verify NHC behavior during
active remediation: selector editing, CR deletion blocking, multi-CR
coordination, and legacy CR name handling. NHC works with any operator
that provides a remediation template CRD; these tests use SNR as the
remediator.

### Prerequisites (Remediation Trigger)

- NHC and SNR operators installed (SNR is used as the remediator in these tests)
- At least 2 Ready worker nodes
- `KUBECONFIG` set with cluster-admin access

### 5. NHC Selector Editing ([OCP-56938](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-56938))

Edits the NHC selector to a non-existent key and verifies observed nodes
drops to 0 without crashing the NHC controller. Also verifies webhook
rejects invalid selector operator values and empty selectors.

- **Operators**: NHC v0.12.0+, SNR
- **Cluster**: Multi-node (2+ workers)
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nhc && disruption:nondestructive" --focus="selector is edited" ./tests/nhc-operator/...`
- **Pass criteria**: Observed nodes drops to 0, NHC remains Enabled, invalid operator value rejected ("is not a valid"), empty selector rejected ("Selector is mandatory"), NHC state unchanged after rejected edits

### 6. NHC Editing and Deletion Blocked During Remediation ([OCP-56600](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-56600))

Stops kubelet via SSH to trigger remediation, then verifies non-selector
fields (minHealthy, unhealthyConditions) remain editable, while NHC
webhook blocks selector editing and CR deletion during active remediation.

- **Operators**: NHC v0.12.0+, SNR
- **Cluster**: Multi-node (2+ workers), SSH access to worker nodes
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nhc && disruption:destructive" --focus="selector editing and deletion" ./tests/nhc-operator/...`
- **Pass criteria**: minHealthy and unhealthyConditions edit succeeds during remediation, selector edit rejected ("selector update prohibited due to running remediation"), CR deletion rejected ("deletion prohibited due to running remediation"), NHC CR still exists and Remediating after delete attempt, SNR remediation completes, node recovers, NHC returns to Enabled

### 7. Old Default NHC CR Name ([OCP-69711](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-69711))

Creates NHC CRs with the legacy name "nhc-worker-default" and a
control-plane NHC, triggers remediation, and verifies the NHC controller
does not crash.

- **Operators**: NHC v0.12.0+, SNR
- **Cluster**: Multi-node (2+ workers)
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nhc && disruption:destructive" --focus="old default NHC CR" ./tests/nhc-operator/...`
- **Pass criteria**: Remediation completes, NHC controller remains Ready

### 8. Only One NHC CR Remediates at a Time ([OCP-66814](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-66814))

Creates two NHC CRs with different remediators (SNR at 30s, TestRemediation
at 10s), stops kubelet via SSH, and verifies only the shorter-duration
TestRemediation NHC creates a remediation CR. The SNR NHC must NOT create
an SNR CR while the node is already being remediated.

- **Operators**: NHC v0.12.0+, SNR
- **Cluster**: Multi-node (2+ workers), SSH access to worker nodes
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nhc && disruption:destructive" --focus="one CR at a time" ./tests/nhc-operator/...`
- **Pass criteria**: TestRemediation CR created for target node, SNR CR NOT created (Consistently), TestRemediation NHC returns to Enabled after kubelet restart

### 9. Non-Remediating NHC CR Deletion During Active Remediation ([OCP-71171](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-71171))

Creates two SNR-based NHC CRs with different unhealthy durations (10s and
11s). The faster NHC triggers SNR remediation first. Verifies the slower
NHC did NOT enter Remediating, then deletes it -- the deletion must succeed.
SNR reboots the node for automatic recovery.

- **Operators**: NHC v0.12.0+, SNR
- **Cluster**: Multi-node (2+ workers), SSH access to worker nodes
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nhc && disruption:destructive" --focus="non-remediating NHC" ./tests/nhc-operator/...`
- **Pass criteria**: Second NHC phase is not Remediating, Delete() succeeds (asserted), first NHC returns to Enabled after SNR remediation, node recovers
