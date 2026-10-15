# AGENTS.md — Test Case Requirements

Requirements and expectations for a test case in this repository. Read this
before writing or modifying a test case document in `docs/TESTCASES/` or a
Ginkgo spec under `tests/<operator>/tests/`.

This file is scoped to test cases. Repository-wide conventions — suite layout,
initialization chain, label taxonomy, commit message format — are in the root
[AGENTS.md](../../AGENTS.md). Behavioral guidelines for agents are in
[.agents/SKILLS.md](../../.agents/SKILLS.md). The directory workflow and status
values are in [README.md](README.md).

## What a test case is

A test case is **two artifacts that ship in the same pull request**:

1. A document, `docs/TESTCASES/rhwa-<jira-id>.md`, generated from the
   [TEMPLATE.md](TEMPLATE.md) skeleton with `make new-testcase JIRA=RHWA-<id>`.
   Always use that target — humans and agents alike. Do not hand-copy the
   skeleton, write a document from memory, or reproduce the section list from
   this file: the target is what guarantees every document starts from the
   current skeleton with all thirteen sections intact. [EXAMPLE.md](EXAMPLE.md)
   is a finished document to read for calibration — never a file to copy, and
   never a source of content for your own test.
2. One or more Ginkgo specs under `tests/<operator>/tests/<feature>.go`.

A spec without a document, or a document whose steps no longer match its spec,
is incomplete work. Do not open a PR with only one of the two.

A third artifact, the entry in tests/<operator>/README.md, is updated in the same PR (see section 13). The operator README is the scannable catalog; the TESTCASES document is the full specification behind that entry.
This applies to tests added from now on. Specs that predate this directory have
no document and do not need one backfilled; when you substantially rework one,
adding its document is welcome but is not a condition of the change.

## Required sections

Every document keeps all thirteen sections of the template, in order, including
the ones that do not apply — write `None` rather than deleting a heading, so
reviewers can tell "not applicable" from "not considered".

| # | Section                   | Must contain                                                                   |
| - | ------------------------- | ------------------------------------------------------------------------------ |
| 1 | Metadata                  | Jira link, `reportxml.ID`, legacy Polarion link or `None`, operator versions, suite, spec file, status |
| 2 | Summary                   | One or two sentences in plain language                                         |
| 3 | Scope                     | In scope **and** an explicit out-of-scope list                                 |
| 4 | Environment               | Topology, platform, storage, namespace, destructive flag, every env var read   |
| 5 | Preconditions             | Conditions asserted in `BeforeAll`, not assumed                                |
| 6 | Test steps                | One row per `By("…")`, in order, with expected result                          |
| 7 | Pass criteria             | What a passing run proves, including that cleanup finished                     |
| 8 | Failure / known issues    | Open bugs, their effect, and the gate used — or `None`                         |
| 9 | Cleanup                   | What `DeferCleanup` and `AfterAll` each remove, and the recovery steps         |
| 10 | Labels                   | The exact `Label(…)` block from the spec                                       |
| 11 | Running it standalone    | A copy-pasteable `ginkgo` command                                              |
| 12 | Implementation sketch    | The spec skeleton, or a link to the merged spec once implemented               |
| 13 | Documentation to update  | Suite `README.md` entry, and any guide that changes                            |

## Expectations for the document

- **Scope boundaries are explicit.** Name the adjacent behaviour this test does
  *not* prove and the spec that covers it. Unstated scope is the most common
  review comment on a new test case.
- **Steps mirror the spec.** The step table and the `By("…")` strings use the
  same wording in the same order. When you change one, change the other.
- **Pass criteria are observable.** "The operator behaves correctly" is not a
  pass criterion; "exactly one `SelfNodeRemediation` named after the target node
  exists within `nhcparams.RemediationCompletionTimeout`" is.
- **Environment is complete and minimal.** List only what the test reads. For a
  new env var, give its name (`<OP>_<THING>`), default, and the behaviour when
  unset — prefer `Skip` with an actionable message over a failure.
- **Destructive is declared.** If the test reboots, fences, cordons, stops a
  kubelet, or injects a network fault, say so in section 4 and label the spec
  `labels.DisruptionDestructive`.
- **Absolute dates and versions.** Pin operator versions (`SBR v0.3.0`), not
  "latest". Write dates as `2026-10-14`, not "last week".

## Expectations for the spec

These are enforced in review and, where noted, by `make lint`.

- Every `It`/`DescribeTable` calls `reportxml.ID("…")` with the ID recorded in
  section 1 — the numeric part of the Jira key for a new test, or the existing
  `OCP-xxxxx` number for a test ported from a Polarion plan.
- Every step is a `By("description")` block.
- `Eventually`/`Consistently` with an explicit timeout and poll interval from
  `<op>params` or `medik8sparams`. Never `time.Sleep`.
- Labels come from `tests/internal/labels` — never a bare string literal.
  Operator, tier, and component are mandatory: CI selects specs with
  `ECO_TEST_LABELS` expressions such as `operator:nhc && tier:smoke`, and a spec
  missing its operator label is silently skipped by those runs. Set frequency,
  disruption, platform, and topology whenever the test is constrained on that
  axis, and keep them consistent with section 4 of the document. Older specs
  predate this rule and do not all satisfy it; new ones must.
- Assertion messages state what *must* hold, naming the resource:
  `"Node %q must reach %s=True after CephFS port injection"`.
- Gomega assertions live in test files only, never in `internal/` packages.
- Preconditions delete stale resources from a previous run **and wait for them to
  be fully gone** before recreating. This is the main source of flakes here.
- Cleanup is idempotent, tolerates `IsNotFound`, and never fails the suite on a
  missing resource — log with `GinkgoWriter.Printf` instead. `DeferCleanup`
  removes what the `It` created; `AfterAll` removes what `BeforeAll` created.
- Recovery (uncordon, restart kubelet, flush iptables, delete injector pods) runs
  even when the spec failed mid-way.
- A spec that exercises an unfixed product bug is gated behind an env var and
  `Skip`s with a message naming the bug and the variable. Never merge a spec
  known to fail on a current build without a gate.
- `make fmt && make lint && make test` pass before pushing.

## Porting a test from Python (ocp-edge-auto)

Read the full Python implementation before writing Go — the test method body,
every helper it calls, and the teardown. The Polarion plan says *what* to verify;
the Python code shows *how*, and the mechanism matters. Record any deviation from
the Python approach, with its reason, in section 3 of the document. See
"Migrating Tests from Python" in the root [AGENTS.md](../../AGENTS.md).

A ported test keeps its existing `OCP-xxxxx` number in `reportxml.ID` so results
stay comparable with historical runs; record the Polarion link in the legacy row
of section 1 and the Jira ticket tracking the port in the Jira row.

## Review checklist

- [ ] Document and spec in the same PR; all thirteen sections present
- [ ] Jira and `reportxml.ID` agree with the spec
- [ ] Out-of-scope list is explicit
- [ ] Step table matches the `By("…")` strings one to one
- [ ] Pass criteria are observable and include cleanup
- [ ] Every env var documented with its default and unset behaviour
- [ ] Destructive tests declared and labelled
- [ ] Labels use `tests/internal/labels` constants; operator, tier, and
      component set, plus every other axis the test is constrained on
- [ ] No `time.Sleep`; timeouts come from `<op>params`/`medik8sparams`
- [ ] Preconditions clean stale state and wait for removal
- [ ] Cleanup idempotent and `IsNotFound`-tolerant
- [ ] Known-bug specs gated behind an env var with a `Skip` message
- [ ] `tests/<operator>/README.md` catalog entry added
- [ ] `make fmt && make lint && make test` pass
