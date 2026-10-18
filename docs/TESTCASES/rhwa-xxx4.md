# RHWA-XXXX — SBR heartbeats on Ceph RBD stay healthy while every agent node is CPU saturated

## 1. Metadata

| Field                        | Value                                                       |
| ---------------------------- | ------------------------------------------------------------- |
| Jira                         | [RHWA-XXXX](https://issues.redhat.com/browse/RHWA-XXXX)     |
| `reportxml.ID`               | `XXXX`                                                      |
| Polarion (legacy) — OPTIONAL | None                                                        |
| Operators                    | SBR v5.8.0                                                  |
| Suite                        | `tests/sbr-operator`                                        |
| Spec file                    | `tests/sbr-operator/tests/block_storage_heartbeat_load.go`  |
| Author                       | rwurmbra                                                    |
| Status                       | draft                                                       |

## 2. Summary

With a Block-mode SBRC on Ceph RBD configured at the tightest heartbeat deadline the CRD allows
(`sbrTimeoutSeconds: 10`, so a 5s interval), saturating the CPUs of every node that runs an agent
must not delay a heartbeat write far enough for a peer to read it as storage loss: no node reports
`SBRStorageUnhealthy=True`, every heartbeat slot keeps advancing, and nothing is remediated.

## 3. Scope

- **In scope**: the false-positive margin of the Block-mode heartbeat path on Ceph RBD under CPU
  contention. Every agent keeps writing its slot, no peer flags anyone, no
  `StorageBasedRemediation` CR appears, no node reboots, and the agents' own
  `sbr_self_fenced_total` and `sbr_device_io_errors_total` counters stand still.
- **Out of scope**:
  - True storage loss, transient or permanent — the opposite direction, covered by the Ceph RBD
    transient partition test, `transient_storage.go` (OCP-88735), `storage_loss_watchdog.go`
    (OCP-88880) and `storage_loss_write_only.go` (OCP-89200).
  - I/O load against the Ceph pool itself, and any other contention than CPU. This test starves
    agents of CPU; it does not slow the device down.
  - Absolute write-latency numbers. See the deviation below.
  - Filesystem-mode (CephFS) heartbeating, which this suite already exercises throughout.

Two deviations from the upstream proposal and from `docs/sbr-block-rbd-test-flows.md`:

- The proposed `p99 < 500ms` heartbeat-latency assertion is dropped: the agent exposes no latency
  metric (`sbrparams.AgentExpectedMetricNames` is the full contract — three counters and two
  gauges), so there is nothing to measure it from without instrumenting the operator. What the
  agent does expose is the outcome that matters, and the test asserts that instead: no peer ever
  reports the node unhealthy, and the device I/O error counter never moves.
- "Every worker" is read as "every node running an agent of this SBRC", derived from the agent pods
  themselves. For a default SBRC that is the same set, and it stays correct if the SBRC carries a
  `nodeSelector`.

## 4. Environment

| Requirement  | Value                                                                                 |
| ------------ | ---------------------------------------------------------------------------------------- |
| Cluster      | OCP ≥ 4.13, multi-node; at least one schedulable worker, and ≥ 2 to exercise peer monitoring |
| Topology     | any                                                                                        |
| Platform     | any (`platform:any`)                                                                       |
| Storage      | Ceph RBD StorageClass (provisioner containing `rbd.csi.ceph.com`), RWX, `volumeMode: Block` |
| Network      | connected or disconnected                                                                  |
| Namespace    | `openshift-workload-availability` (`medik8sparams.OperatorNs`)                            |
| Destructive? | no — nothing is rebooted, fenced, cordoned or network-isolated. Every agent node does run its CPUs at saturation for up to 5 minutes, which will slow co-located workloads for that window, so the spec is labelled `frequency:weekly` and belongs in a job that tolerates it |

Environment variables:

```bash
export KUBECONFIG=/absolute/path/to/kubeconfig
export ECO_TEST_FEATURES=sbr-operator
export ECO_TEST_LABELS='operator:sbr && tier:resiliency'
export ECO_REPORTS_DUMP_DIR="$(mktemp -d)"

# Optional. Unset, the test auto-discovers a StorageClass whose provisioner contains
# "rbd.csi.ceph.com" and skips when the cluster has none.
export SBR_RBD_STORAGE_CLASS=ocs-storagecluster-ceph-rbd

# Optional. Image for the stress pods; must carry sh and nproc. Defaults to
# registry.access.redhat.com/ubi9/ubi:latest, which needs a mirror in a disconnected cluster.
export SBR_INJECTOR_IMAGE=registry.example/ubi9/ubi:latest
```

## 5. Preconditions

Asserted in `BeforeAll`:

1. No `NodeHealthCheck` remediates through `StorageBasedRemediationTemplate`. One would be entitled
   to remediate a node that did report unhealthy, which would confuse a test whose whole claim is
   that nothing was remediated, so the spec skips rather than fails.
2. Self Node Remediation is not running: SNR and SBR cannot both hold `/dev/watchdog`.
3. The cluster has a Ceph RBD StorageClass, auto-discovered or named by `SBR_RBD_STORAGE_CLASS`.
4. Any SBRC left by an earlier run is deleted and gone before a fresh one is created.
5. A Block-mode SBRC with `sbrTimeoutSeconds: 10` exists on that StorageClass and every pod of its
   agent DaemonSet is Ready.
6. At least one Running agent pod is available to serve as the observer that device reads go
   through.

## 6. Test steps

| #  | Step (`By` text)                                                                      | Expected result                                                                          |
| -- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| 1  | Recording the boot-id of every node running an SBR agent                                  | `status.nodeInfo.bootID` is read and non-empty for each of them                              |
| 2  | Recording each agent's sbr_self_fenced_total counter                                      | Each agent's `:8082/metrics` is scraped once and the counter is present                      |
| 3  | Recording each agent's sbr_device_io_errors_total counter                                 | The counter is parsed out of the same scrape                                                 |
| 4  | Recording every node's heartbeat sequence number through the observer agent               | At least one written slot is parsed out of the heartbeat region                              |
| 5  | Starting a CPU stress pod on every node running an SBR agent                              | One pod per agent node, each spinning `nproc` busy loops                                     |
| 6  | Waiting for every stress pod to be Running                                                | All reach Running within `medik8sparams.DefaultTimeout`                                      |
| 7  | Consistently asserting for 3m0s that no node reports SBRStorageUnhealthy=True             | No agent node holds the condition `True` at any poll in the window                           |
| 8  | Asserting every node's heartbeat sequence number advanced through the observer agent      | No node ID's sequence is unchanged or missing compared with step 4                           |
| 9  | Asserting no StorageBasedRemediation CR exists                                            | The cluster-wide CR count is zero                                                            |
| 10 | Asserting every node still has the boot-id it started with                                | Each boot-id equals the one from step 1                                                      |
| 11 | Asserting each agent's sbr_self_fenced_total counter is unchanged                         | Each equals the value from step 2                                                            |
| 12 | Asserting each agent's sbr_device_io_errors_total counter is unchanged                    | Each equals the value from step 3                                                            |

## 7. Pass criteria

- Exactly one spec with `reportxml.ID("XXXX")` runs and passes.
- For the whole of `sbrparams.BlockLoadCheckDuration` under load, no node running an agent reports
  `SBRStorageUnhealthy=True`.
- Every heartbeat slot seen before the load has a strictly higher sequence number after it, read
  through the observer agent: the agents kept writing rather than all stalling together.
- No `StorageBasedRemediation` CR exists in the cluster, and every agent node still has the boot-id
  it had before the load.
- Every agent's `sbr_self_fenced_total` and `sbr_device_io_errors_total` are unchanged.
- Cleanup finishes: every stress pod is gone and the SBRC and its agent DaemonSet are removed.

## 8. Failure / known issues

None.

## 9. Cleanup

- `DeferCleanup` deletes the stress pod created on each agent node and waits for each to be gone.
  Each pod also stops its own load after `sbrparams.BlockLoadStressDuration` and exits, so a run
  that dies before cleanup cannot leave a node saturated.
- `AfterAll` removes the SBRC created in `BeforeAll`, tolerating `IsNotFound`, and waits for it to
  be gone so the next suite does not inherit its agent DaemonSet.
- No node state is changed, so there is nothing to uncordon, untaint or restart.

## 10. Labels

```go
Label(
    labels.OperatorSBR,
    labels.TierResiliency,
    labels.ComponentDaemonSet,
    labels.DisruptionNonDestructive,
    labels.PlatformAny,
    labels.FrequencyWeekly,
)
```

## 11. Running it standalone

```bash
ginkgo -v --label-filter="operator:sbr && tier:resiliency" \
    --focus="heartbeats remain stable on Ceph RBD under load" ./tests/sbr-operator/...
```

## 12. Implementation sketch

Implemented in
[`tests/sbr-operator/tests/block_storage_heartbeat_load.go`](../../tests/sbr-operator/tests/block_storage_heartbeat_load.go),
on the device, pod and metric helpers in
[`block_storage_partition_helpers.go`](../../tests/sbr-operator/tests/block_storage_partition_helpers.go)
and
[`block_storage_rbd_helpers.go`](../../tests/sbr-operator/tests/block_storage_rbd_helpers.go).

```go
It("Verify SBR heartbeats remain stable on Ceph RBD under load",
    reportxml.ID("XXXX"),
    Label(
        labels.OperatorSBR,
        labels.TierResiliency,
        labels.ComponentDaemonSet,
        labels.DisruptionNonDestructive,
        labels.PlatformAny,
        labels.FrequencyWeekly,
    ), func() {
        baselines := captureAgentBaselines()
        sequences := readBaselineSequences(observerPodName)
        startCPULoad(baselines)
        assertNoAgentUnhealthyUnderLoad(baselines)
        assertHeartbeatsAdvanced(observerPodName, sequences)
        assertNothingRemediatedUnderLoad(baselines)
        assertAgentCountersUnchanged(baselines)
    })
```

Three numbers carry the test. `sbrTimeoutSeconds: 10` is the CRD minimum, halving to the 5s
heartbeat interval that gives the agent the least slack. `sbrparams.BlockLoadCheckDuration` (3
minutes) is roughly 36 heartbeat intervals, long enough that a systematic delay surfaces rather
than being missed between polls. `sbrparams.BlockLoadStressDuration` (5 minutes) outlasts that
window plus the assertions that run after it, while still bounding the load on its own.

## 13. Documentation to update

- `tests/sbr-operator/README.md` — catalog entry 15, with the Jira link, requirements, standalone
  command and pass criteria.
- `docs/README.md` — not needed; this adds no cross-cutting guide.
