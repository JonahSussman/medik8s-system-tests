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

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology (MNO or SNO)
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="pod is running" ./tests/far-operator/...`
- **Pass criteria**: All pods Running, count matches expected replicas for the topology

### 2. Verify FAR CSV Has Required Annotations (Polarion 70637)

Validates that the active FAR ClusterServiceVersion (in Succeeded phase) has all required OLM feature annotations: disconnected support, FIPS compliance, suggested namespace, and feature flags.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="required annotations" ./tests/far-operator/...`
- **Pass criteria**: All required annotations present with expected values on the active CSV

### 3. Verify FAR Controller Replicas and Node Distribution (Polarion 61222)

Validates that 2 replicas are running and scheduled on different nodes for high availability. Skipped on SNO clusters where only 1 replica is expected.

- **Operators**: FAR v0.8.0
- **Cluster**: Multi-node only (skips on SNO)
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="correct number of replicas" ./tests/far-operator/...`
- **Pass criteria**: 2 ready replicas on 2 different nodes

### 4. Verify FAR Container Security Context (Polarion 89231)

Validates the manager container follows the restricted security posture: runAsNonRoot at pod level, runAsUser is not UID 0 when set, allowPrivilegeEscalation=false, capabilities.drop=ALL, readOnlyRootFilesystem=true, and seccompProfile=RuntimeDefault (at container or pod level). Only checks the `manager` container, not sidecars.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="non-root user" ./tests/far-operator/...`
- **Pass criteria**: All security context fields match expected restricted profile

### 5. Verify FAR CRDs Are Installed and Established (Polarion 89548)

Validates that both FAR Custom Resource Definitions are registered as cluster-level resources and have the `Established=True` status condition, confirming the API endpoints are active and ready for clients.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="CRDs are installed" ./tests/far-operator/...`
- **Pass criteria**: Both CRDs (`fenceagentsremediations` and `fenceagentsremediationtemplates`) exist with Established=True

### 6. Verify FAR Operator Namespace Has Correct PSA Enforcement Label (Polarion 89549)

Validates that the operator namespace (`openshift-workload-availability`) has the correct Pod Security Admission enforcement label set to `privileged`, ensuring the namespace admission policy allows the operator pods to run with required permissions.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="PSA enforcement label" ./tests/far-operator/...`
- **Pass criteria**: Namespace has `pod-security.kubernetes.io/enforce=privileged` label

### 7. Verify FAR Controller Has system-cluster-critical Priority Class (Polarion 66211)

Validates that all FAR controller-manager pods have `priorityClassName` set to `system-cluster-critical`, ensuring the controller retains scheduling priority during node pressure events.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="priority class" ./tests/far-operator/...`
- **Pass criteria**: All running FAR pods have `priorityClassName: system-cluster-critical`

### 8. Verify FAR Controller Pod Has Correct Kubernetes Labels (Polarion 66209)

Validates that FAR controller-manager pods carry the standard `app.kubernetes.io/name` label with the correct value, ensuring service discovery and monitoring tools can identify FAR pods.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="Kubernetes labels" ./tests/far-operator/...`
- **Pass criteria**: All running FAR pods have `app.kubernetes.io/name=fence-agents-remediation-operator`

### 9. Verify FAR Controller Container Includes Expected Fence Agents (Polarion 78407)

Validates that the FAR controller container image ships the minimum expected set of fence agent binaries in `/usr/sbin/`. Execs into the container and lists all `fence_*` binaries, then checks that a core subset (fence_aws, fence_azure_arm, fence_gce, fence_ipmilan, fence_kubevirt, fence_redfish) is present.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="fence agents" ./tests/far-operator/...`
- **Pass criteria**: All expected fence agent binaries are present in the container
