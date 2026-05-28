# FAR Operator Post-Deployment Tests

Automated tests validating the Fence Agents Remediation (FAR) operator deployment, security posture, and high-availability configuration.

## Prerequisites

- OpenShift cluster with FAR operator installed via OLM
- `KUBECONFIG` set with cluster-admin access
- FAR installed in `openshift-workload-availability` namespace

## Running

```bash
ginkgo --label-filter="far" ./tests/far-operator/...
```

## Tests

### 1. Verify FAR Operator Pod is Running (Polarion 66026)

Validates that FAR controller-manager pods are in Running state and the pod count matches the cluster topology (2 on multi-node, 1 on SNO).

- **Operators**: FAR (any version)
- **Cluster**: Any topology (MNO or SNO)
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="pod is running" ./tests/far-operator/...`
- **Pass criteria**: All pods Running, count matches expected replicas for the topology

### 2. Verify FAR CSV Has Required Annotations (Polarion 70637)

Validates that the active FAR ClusterServiceVersion (in Succeeded phase) has all required OLM feature annotations: disconnected support, FIPS compliance, suggested namespace, and feature flags.

- **Operators**: FAR (any version)
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="required annotations" ./tests/far-operator/...`
- **Pass criteria**: 8 required annotations present with expected values on the active CSV

### 3. Verify FAR Controller Replicas and Node Distribution (Polarion 61222)

Validates that 2 replicas are running and scheduled on different nodes for high availability. Skipped on SNO clusters where only 1 replica is expected.

- **Operators**: FAR (any version)
- **Cluster**: Multi-node only (skips on SNO)
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="correct number of replicas" ./tests/far-operator/...`
- **Pass criteria**: 2 ready replicas on 2 different nodes

### 4. Verify FAR Container Security Context (Polarion 61208)

Validates the manager container follows the restricted security posture: runAsNonRoot at pod level, allowPrivilegeEscalation=false, capabilities.drop=ALL, and seccompProfile=RuntimeDefault (at container or pod level). Only checks the `manager` container, not sidecars.

- **Operators**: FAR (any version)
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="non-root user" ./tests/far-operator/...`
- **Pass criteria**: All security context fields match expected restricted profile
