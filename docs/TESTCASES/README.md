# Test Case Documents

Every system test in this repository is described by a test case document in this
directory before — or alongside — its Ginkgo spec. The document is the contract a
reviewer reads: what is verified, on which cluster, and what counts as a pass.

The normative rules live in [AGENTS.md](AGENTS.md) and apply to humans and coding
agents alike. This file explains the directory and the workflow.

## Contents

| File                                   | Purpose                                                 |
| -------------------------------------- | --------------------------------------------------------- |
| [AGENTS.md](AGENTS.md)                 | Requirements and expectations for a test case             |
| [rhwa-1234.example](rhwa-1234.example) | Filled-in template to copy                                |
| `rhwa-<jira-id>.md`                    | One document per Jira ticket                              |

## Naming

- One file per Jira ticket: `rhwa-<jira-id>.md`, lowercase (`rhwa-1834.md`).
- A ticket covering several specs stays in one document, with one
  "Test steps"/"Pass criteria" pair per spec.
- `rhwa-1234.example` is the template. It is never a real test case; leave it
  alone except to improve the template itself.

## Workflow

1. **Copy the template.** `cp rhwa-1234.example rhwa-<jira-id>.md`, then replace
   every value and delete the `>` guidance blockquotes.
2. **Fill in sections 1–11 before writing Go.** Scope, environment, preconditions,
   numbered steps, and pass criteria are design decisions, not documentation of
   code that already exists. Getting them reviewed first is cheaper than
   rewriting a spec.
3. **Implement the spec** under `tests/<operator>/tests/<feature>.go`, keeping the
   `By("…")` strings identical to the step table.
4. **Update the suite catalog.** Add the test to `tests/<operator>/README.md` with
   its Polarion link, requirements, standalone command, and pass criteria.
5. **Set the document status** to `implemented` and open the PR with the document
   and the spec in the same change.

## Status values

| Status           | Meaning                                                              |
| ---------------- | ---------------------------------------------------------------------- |
| `draft`          | Document under review; no spec merged yet                             |
| `implemented`    | Spec merged and passing in CI                                         |
| `blocked-by-bug` | Spec merged but gated behind an env var because the product bug is open |

## Reviewing a test case

A reviewer should be able to read the document next to the spec and match them
line for line. The checklist is in [AGENTS.md](AGENTS.md#review-checklist).

## Related

- [AGENTS.md](../../AGENTS.md) — repository-wide conventions, suite layout, label
  taxonomy, commit message format
- [docs/README.md](../README.md) — cross-cutting guides
- [docs/fbc-upgrades.md](../fbc-upgrades.md) — shared FBC upgrade contract
