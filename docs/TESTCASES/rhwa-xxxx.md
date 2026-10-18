# RHWA-XXXX — A transient Ceph RBD network partition clears without fencing the node

## 1. Metadata

| Field                        | Value                                                            |
| ---------------------------- | ---------------------------------------------------------------- |
| Jira                         | [RHWA-XXXX](https://issues.redhat.com/browse/RHWA-XXXX)          |
| `reportxml.ID`               | `XXXX`                                                           |
| Polarion (legacy) — OPTIONAL | None                                                             |
| Operators                    | SBR v5.8.0                                                       |
| Suite                        | `tests/sbr-operator`                                             |
| Spec file                    | `tests/sbr-operator/tests/block_storage_transient_partition.go`  |
| Author                       | rwurmbra                                                         |
| Status                       | draft                                                            |

## 2. Summary

When a worker node briefly loses its Ceph RBD traffic, a peer agent must report the node
`SBRStorageUnhealthy=True`, and the condition must clear again on its own once the traffic returns —
without the node rebooting, self-fencing, or acquiring a `StorageBasedRemediation` CR.

## 3. Scope

- **In scope**: detection and self-healing of a *transient* storage outage on a Block-mode SBRC
  backed by Ceph RBD. The node condition goes `True` (reason `HeartbeatTimeout`) and back to `False`
  (reason `Recovered`); the victim's heartbeat slot stalls and then resumes; nothing fences the node.
- **Out of scope**:
  - The same scenario on CephFS, which `transient_storage.go` (OCP-88735) already covers. This test
    exists because Block mode drives a different I/O path: a raw RBD device with O_DIRECT slot
    writes and no filesystem or MDS in between.
  - A *permanent* storage loss, where the watchdog must fire and the node must reboot
    (`storage_loss_watchdog.go`, OCP-88880, and `storage_loss_write_only.go`, OCP-89200).
  - The NHC → `StorageBasedRemediation` → fencing path (`nhc_integration.go`, OCP-88879). This test
    deliberately runs with no NHC pointed at the SBR template and skips when one exists; without
    that, NHC could legitimately remediate the node and the "no fencing" assertions would be wrong.
  - Fence-state durability across a reboot on Block mode (RHWA-1916) and concurrent multi-writer
    access to the shared Block device (RHWA-1915).

Two deviations from the behaviour described in `docs/sbr-block-rbd-test-flows.md`, both in how the
victim's heartbeat slot is found and watched:

- Nothing in the API maps a node name to the SBD node ID that owns its heartbeat slot, and the
  on-device node map is a CRC-framed JSON blob this suite has no reader for. The spec therefore
  records every slot's sequence number before the outage (step 4) and identifies the victim's slot
  as the one that falls far behind its peers while it is partitioned (step 8) - "far behind"
  rather than "frozen" because the baseline is read before the rules are injected, so the victim
  normally lands one more heartbeat first. That also asserts something worth asserting on its own:
  only the partitioned node stops keeping up.
- The three post-recovery checks — boot-id, Ready, no remediation CR — are a single `Consistently`
  block (step 12), as the flow document allows.

## 4. Environment

| Requirement  | Value                                                                               |
| ------------ | ------------------------------------------------------------------------------------- |
| Cluster      | OCP ≥ 4.13, multi-node: at least 2 schedulable workers, one of which hosts no SBR controller pod |
| Topology     | `topology:minimal-worker`                                                            |
| Platform     | any (`platform:any`)                                                                  |
| Storage      | Ceph RBD StorageClass (provisioner containing `rbd.csi.ceph.com`), RWX, `volumeMode: Block` |
| Network      | connected or disconnected                                                             |
| Namespace    | `openshift-workload-availability` (`medik8sparams.OperatorNs`)                        |
| Destructive? | yes — Ceph traffic is blocked on one worker node for 150s with host iptables rules, and that node is expected to recover by itself |

Environment variables:

```bash
export KUBECONFIG=/absolute/path/to/kubeconfig
export ECO_TEST_FEATURES=sbr-operator
export ECO_TEST_LABELS='operator:sbr && tier:acceptance'
export ECO_REPORTS_DUMP_DIR="$(mktemp -d)"

# Optional. Unset, the test auto-discovers a StorageClass whose provisioner contains
# "rbd.csi.ceph.com" and skips when the cluster has none.
export SBR_RBD_STORAGE_CLASS=ocs-storagecluster-ceph-rbd

# Optional. Image for the privileged disruptor, probe and flush pods; must carry nsenter and
# iptables. Defaults to registry.access.redhat.com/ubi9/ubi:latest, which needs a mirror in a
# disconnected cluster.
export SBR_INJECTOR_IMAGE=registry.example/ubi9/ubi:latest
```

## 5. Preconditions

Asserted in `BeforeAll`:

1. No `NodeHealthCheck` remediates through `StorageBasedRemediationTemplate`. One would create a
   `StorageBasedRemediation` CR for the partitioned node and fence it, which is correct NHC
   behaviour but the opposite of what this test measures — so the spec skips rather than fails.
2. Self Node Remediation is not running: SNR and SBR cannot both hold `/dev/watchdog`.
3. The cluster has a Ceph RBD StorageClass, auto-discovered or named by `SBR_RBD_STORAGE_CLASS`.
4. Any SBRC left by an earlier run is deleted and gone before a fresh one is created.
5. A Block-mode SBRC on that StorageClass exists and every pod of its agent DaemonSet is Ready.
6. A schedulable worker node that hosts no SBR controller pod is available as the victim, and a
   second agent pod on a different node is available as the observer. Device reads go through the
   observer, so they keep working while the victim is cut off.
7. `iptables` is usable in the victim's host namespaces — proved with a throwaway privileged pod
   that lists the host `OUTPUT` chain, so an nftables-only node skips instead of failing mid-test.

## 6. Test steps

| #  | Step (`By` text)                                                                                                            | Expected result                                                                           |
| -- | ---------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| 1  | Recording the victim node's boot-id                                                                                          | `status.nodeInfo.bootID` is read and non-empty                                               |
| 2  | Recording the victim agent's sbr_self_fenced_total counter and container restart count                                       | `sbr_self_fenced_total` is present on the agent's `:8082/metrics`; restart count is read     |
| 3  | Asserting SBRStorageUnhealthy is not True on the victim node                                                                 | The condition is absent or `False`                                                           |
| 4  | Recording every node's heartbeat sequence number through the observer agent                                                  | At least one written slot is parsed out of the heartbeat region                              |
| 5  | Starting a disruptor pod that blocks Ceph ports 3300, 6789 and 6800-7300 on the victim node for 2m30s                        | The privileged pod reaches Running on the victim node                                        |
| 6  | Waiting for SBRStorageUnhealthy=True with reason HeartbeatTimeout on the victim node                                         | A peer sets the condition within `sbrparams.StorageInjectionTimeout`                         |
| 7  | Asserting no StorageBasedRemediation CR exists for the victim node                                                           | `StorageBasedRemediation/<victim>` is `IsNotFound`                                           |
| 8  | Confirming exactly one heartbeat slot fell behind while the victim is partitioned                                           | Exactly one node ID advanced by less than a quarter of the median advance; that is the victim's slot |
| 9  | Waiting for the disruptor pod to complete and restore Ceph traffic                                                           | The pod reaches `Succeeded`, having removed its own REJECT rules in its EXIT trap            |
| 10 | Waiting for SBRStorageUnhealthy=False with reason Recovered on the victim node                                               | The condition clears within `sbrparams.BlockPartitionRecoveryTimeout`                        |
| 11 | Asserting the victim's heartbeat sequence number is increasing again through the observer agent                              | That slot's sequence passes its pre-outage value within `sbrparams.BlockHeartbeatResumeTimeout` |
| 12 | Consistently asserting for 1m30s that the victim keeps its boot-id, stays Ready and gets no StorageBasedRemediation CR       | Boot-id unchanged, `Ready=True`, and no CR for the whole window                              |
| 13 | Asserting the victim agent's sbr_self_fenced_total counter and container restart count are unchanged                         | Both equal the values recorded in step 2                                                     |

## 7. Pass criteria

- Exactly one spec with `reportxml.ID("XXXX")` runs and passes.
- The victim node reports `SBRStorageUnhealthy=True` with reason `HeartbeatTimeout` while its Ceph
  ports are blocked, and `False` with reason `Recovered` within
  `sbrparams.BlockPartitionRecoveryTimeout` of the block being lifted.
- Exactly one heartbeat slot falls behind during the outage, and that slot's sequence number
  passes the value it stalled at within `sbrparams.BlockHeartbeatResumeTimeout` of recovery.
- Across `sbrparams.BlockNoFencingCheckDuration` after recovery the victim keeps the boot-id it had
  before the partition, stays `Ready=True`, and has no `StorageBasedRemediation` CR.
- The victim agent's `sbr_self_fenced_total` and container restart count are the same before and
  after, so the node neither self-fenced nor lost its agent.
- Cleanup finishes: the disruptor pod is gone, no Ceph REJECT rule is left on the victim, no
  `StorageBasedRemediation` CR remains, and the SBRC and its agent DaemonSet are removed.

## 8. Failure / known issues

None.

## 9. Cleanup

- `DeferCleanup` deletes the disruptor pod the `It` created and then runs a short-lived flush pod on
  the victim that deletes each Ceph REJECT rule with `|| true`, so the node comes back clean even if
  the spec failed mid-outage. The disruptor also removes its own rules from a `sh` EXIT trap, with
  its sleep backgrounded so the trap still fires when the pod is deleted early: the partition can
  outlive neither the pod nor the test.
- `AfterAll` removes any `StorageBasedRemediation` CR left for the victim (finalizers cleared first)
  and the SBRC created in `BeforeAll`, tolerating `IsNotFound`, then waits for the SBRC to be gone.
- The probe and flush pods delete themselves at the end of the helper that runs them, and every
  host-command pod deletes a same-named leftover before it starts.

## 10. Labels

```go
Label(
    labels.OperatorSBR,
    labels.TierAcceptance,
    labels.ComponentRemediation,
    labels.DisruptionDestructive,
    labels.PlatformAny,
    labels.FrequencyNightly,
)
```

## 11. Running it standalone

```bash
ginkgo -v --label-filter="operator:sbr && tier:acceptance" \
    --focus="transient Ceph RBD network partition" ./tests/sbr-operator/...
```

## 12. Implementation sketch

Implemented in
[`tests/sbr-operator/tests/block_storage_transient_partition.go`](../../tests/sbr-operator/tests/block_storage_transient_partition.go),
with the device, pod and metric helpers in
[`block_storage_partition_helpers.go`](../../tests/sbr-operator/tests/block_storage_partition_helpers.go).

```go
It("Verify SBR recovers from a transient Ceph RBD network partition without fencing",
    reportxml.ID("XXXX"),
    Label(
        labels.OperatorSBR,
        labels.TierAcceptance,
        labels.ComponentRemediation,
        labels.DisruptionDestructive,
        labels.PlatformAny,
        labels.FrequencyNightly,
    ), func() {
        baseline := captureVictimBaseline(victimNodeName, observerPodName)
        startCephRBDPartition(victimNodeName)
        victimSlot := awaitPartitionDetected(victimNodeName, observerPodName, baseline)
        awaitPartitionRecovery(victimNodeName)
        assertVictimNeverFenced(victimNodeName, observerPodName, baseline, victimSlot)
    })
```

The outage length is the one tuning decision worth repeating here. With the SBRC defaults
(`sbrTimeoutSeconds: 30`, so a 15s heartbeat interval) a peer needs
`maxConsecutiveFailures × 15s` of silence before it sets the condition, and the agent downgrades the
condition to `Unknown` (reason `GivingAgentChance`) once it has held `True` for
`(maxConsecutiveFailures + 1) × 15s + 5s`. `sbrparams.BlockPartitionOutage` is 150s, between the
two: shorter and the condition never appears, longer and it is `Unknown` rather than `Recovered`.

## 13. Documentation to update

- `tests/sbr-operator/README.md` — catalog entry 14, with the Jira link, requirements, standalone
  command and pass criteria.
- `docs/README.md` — not needed; this adds no cross-cutting guide.
