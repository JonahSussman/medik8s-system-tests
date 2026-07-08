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
