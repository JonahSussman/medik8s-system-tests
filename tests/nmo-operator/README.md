# NMO Operator Post-Deployment Tests

Automated tests validating the Node Maintenance Operator (NMO)
deployment, OLM metadata, and security posture.

## Prerequisites

- OpenShift cluster with NMO operator installed via OLM
- `KUBECONFIG` set with cluster-admin access
- NMO installed in `openshift-workload-availability` namespace

## Running

```bash
ginkgo --label-filter="nmo" ./tests/nmo-operator/...
```

Or via the test runner:

```bash
export KUBECONFIG=/path/to/kubeconfig
export ECO_TEST_FEATURES="nmo-operator"
make run-tests
```

## Tests

### 1. Verify Node Maintenance Operator Pod Is Running (Polarion OCP-46315)

Validates that the NMO controller-manager pod is in Running state
with all containers ready.

- **Operators**: NMO v0.17.0+
- **Cluster**: Any topology (MNO or SNO)
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nmo" --focus="pod is running" ./tests/nmo-operator/...`
- **Pass criteria**: At least 1 running NMO controller pod with all containers ready

### 2. Verify NMO CSV Has Required Annotations

Validates that the active NMO ClusterServiceVersion (in Succeeded phase)
has all required OLM infrastructure annotations with expected values.

- **Operators**: NMO v0.17.0+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nmo" --focus="CSV has required annotations" ./tests/nmo-operator/...`
- **Pass criteria**: All 8 required annotations present with correct values (disconnected, fips-compliant, proxy-aware, tls-profiles, token-auth-aws/azure/gcp, suggested-namespace)

### 3. Verify NMO Controller Manager Has Correct Number of Replicas

Validates that the NMO deployment has the expected replica count
and all replicas are ready. NMO runs a single replica on all
cluster topologies (MNO and SNO).

- **Operators**: NMO v0.17.0+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nmo" --focus="correct number of replicas" ./tests/nmo-operator/...`
- **Pass criteria**: spec.replicas == 1 and status.readyReplicas == 1

### 4. Verify NMO Container Runs as Non-Root User

Validates that the NMO manager container enforces a restricted
security context: runAsNonRoot, no privilege escalation,
all capabilities dropped, and RuntimeDefault seccomp profile.

- **Operators**: NMO v0.17.0+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="nmo" --focus="runs as non-root" ./tests/nmo-operator/...`
- **Pass criteria**: Pod runAsNonRoot=true, manager container allowPrivilegeEscalation=false, capabilities.drop=[ALL], seccomp profile RuntimeDefault
