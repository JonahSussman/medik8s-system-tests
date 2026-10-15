#!/usr/bin/env bash
# Structural check for the test case documents in docs/TESTCASES/.
#
# Usage:
#   scripts/check-testcase.sh [file ...]
#
# With no arguments it checks TEMPLATE.md and every docs/TESTCASES/rhwa-*.md.
#
# This is deliberately lenient. It checks the few things a machine can check
# without judgement -- the thirteen sections, the metadata rows CI and the
# Polarion/Jira reporting depend on, and leftover template placeholders. Whether
# the scope is well drawn, the steps mirror the spec, or the pass criteria are
# observable is for a human reviewer; see docs/TESTCASES/AGENTS.md.
#
# Errors fail the run. Warnings are printed and do not.
#
# Kept portable between GNU and BSD userland (Linux and macOS): no bash 4
# features, no GNU-only flags.

set -uo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)

errors=0
warnings=0

# GitHub renders ::error/::warning lines as annotations on the PR; outside CI
# they are just readable log lines.
report() {
    level="$1"
    file="$2"
    message="$3"

    if [ -n "${GITHUB_ACTIONS:-}" ]; then
        printf '::%s file=%s::%s\n' "${level}" "${file}" "${message}"
    else
        printf '%s: %s: %s\n' "${level}" "${file}" "${message}"
    fi
}

err() {
    report error "$1" "$2"
    errors=$((errors + 1))
}

warn() {
    report warning "$1" "$2"
    warnings=$((warnings + 1))
}

section_title() {
    case "$1" in
    1) echo "Metadata" ;;
    2) echo "Summary" ;;
    3) echo "Scope" ;;
    4) echo "Environment" ;;
    5) echo "Preconditions" ;;
    6) echo "Test steps" ;;
    7) echo "Pass criteria" ;;
    8) echo "Failure / known issues" ;;
    9) echo "Cleanup" ;;
    10) echo "Labels" ;;
    11) echo "Running it standalone" ;;
    12) echo "Implementation sketch" ;;
    13) echo "Documentation to update" ;;
    esac
}

# Value cell of a metadata table row, matched on the start of the field cell so
# "Polarion (legacy) - OPTIONAL" is found by "Polarion".
meta_value() {
    awk -F'|' -v want="$2" '
        /^[ \t]*\|/ {
            field = $2
            value = $3
            gsub(/^[ \t]+|[ \t]+$/, "", field)
            gsub(/^[ \t]+|[ \t]+$/, "", value)
            gsub(/`/, "", field)
            if (index(field, want) == 1) { print value; exit }
        }' "$1"
}

strip_backticks() {
    printf '%s' "$1" | tr -d '`' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'
}

check_sections() {
    file="$1"
    missing=0

    n=1
    while [ "${n}" -le 13 ]; do
        if ! grep -Eq "^## ${n}\. " "${file}"; then
            err "${file}" "section ${n} ($(section_title "${n}")) is missing; keep every heading and write None under the ones that do not apply"
            missing=$((missing + 1))
        else
            # Present, but renamed: worth a nudge, not a failure.
            expected=$(section_title "${n}")
            actual=$(grep -E "^## ${n}\. " "${file}" | head -1 | sed -E "s/^## ${n}\. *//")
            if [ "$(printf '%s' "${actual}" | tr '[:upper:]' '[:lower:]')" != "$(printf '%s' "${expected}" | tr '[:upper:]' '[:lower:]')" ]; then
                warn "${file}" "section ${n} is titled '${actual}'; the template calls it '${expected}'"
            fi
        fi
        n=$((n + 1))
    done

    if [ "${missing}" -eq 0 ]; then
        order=$(grep -E '^## [0-9]+\. ' "${file}" | sed -E 's/^## ([0-9]+)\..*/\1/' | tr '\n' ' ')
        if [ "${order}" != "1 2 3 4 5 6 7 8 9 10 11 12 13 " ]; then
            err "${file}" "sections are out of order or duplicated: found '${order% }'"
        fi
    fi
}

check_metadata() {
    file="$1"
    base=$(basename "${file}" .md)

    # Empty when the filename is malformed; the comparisons below are skipped
    # rather than repeating a complaint already made about the name.
    jira_number=""
    if printf '%s' "${base}" | grep -Eq '^rhwa-[0-9]+$'; then
        jira_number=$(printf '%s' "${base}" | sed -E 's/^rhwa-//')
    fi

    jira=$(meta_value "${file}" "Jira")
    report_id=$(strip_backticks "$(meta_value "${file}" "reportxml.ID")")
    polarion=$(strip_backticks "$(meta_value "${file}" "Polarion")")
    status=$(strip_backticks "$(meta_value "${file}" "Status")")
    suite=$(strip_backticks "$(meta_value "${file}" "Suite")")
    spec=$(strip_backticks "$(meta_value "${file}" "Spec file")")

    # Jira: the system of record. Must be there and must look like a ticket.
    if printf '%s' "${jira}" | grep -Eqv 'RHWA-[0-9]+'; then
        err "${file}" "metadata has no Jira ticket; section 1 needs a RHWA-<id> link"
    elif [ -n "${jira_number}" ] &&
        [ "$(printf '%s' "${jira}" | sed -E 's/.*RHWA-([0-9]+).*/\1/')" != "${jira_number}" ]; then
        warn "${file}" "Jira ticket does not match the filename (expected RHWA-${jira_number})"
    fi

    # reportxml.ID: what ties a run in CI back to this document.
    if [ -z "${report_id}" ]; then
        err "${file}" "metadata has no reportxml.ID; every It/DescribeTable needs one"
    elif printf '%s' "${report_id}" | grep -Eqv '^[0-9]+$'; then
        err "${file}" "reportxml.ID '${report_id}' should be digits only (the Jira number, or the legacy OCP number for a ported test)"
    elif [ -n "${jira_number}" ] && [ "${report_id}" != "${jira_number}" ]; then
        # Legitimate for a ported test that keeps its Polarion number.
        case "${polarion}" in
        "" | None | none)
            warn "${file}" "reportxml.ID '${report_id}' is neither the Jira number (${jira_number}) nor paired with a legacy Polarion ID"
            ;;
        esac
    fi

    # Polarion is optional and intentionally unchecked beyond this.

    case "${status}" in
    draft | implemented | blocked-by-bug) ;;
    "")
        err "${file}" "metadata has no Status; use draft, implemented, or blocked-by-bug"
        ;;
    *)
        err "${file}" "Status '${status}' is not one of draft, implemented, blocked-by-bug"
        ;;
    esac

    if [ -z "${suite}" ]; then
        warn "${file}" "metadata has no Suite row"
    fi

    if [ -z "${spec}" ]; then
        warn "${file}" "metadata has no Spec file row"
    elif [ "${status}" = "implemented" ] && [ ! -f "${REPO_ROOT}/${spec}" ]; then
        warn "${file}" "Status is implemented but spec file '${spec}' does not exist"
    fi

    # A blocked-by-bug document should say which bug, in section 8.
    if [ "${status}" = "blocked-by-bug" ]; then
        body=$(sed -n '/^## 8\./,/^## 9\./p' "${file}" |
            grep -v '^## ' | grep -v '^>' | grep -v '^[[:space:]]*$')
        if [ "$(printf '%s' "${body}" | tr -d '[:space:].')" = "None" ]; then
            err "${file}" "Status is blocked-by-bug but section 8 says None; name the bug and the env var gating the spec"
        fi
    fi
}

check_leftovers() {
    file="$1"

    # Only the placeholders the skeleton actually ships, so a document is free
    # to use angle brackets of its own in a code block.
    placeholders='RHWA-XXXX|`XXXX`|reportxml\.ID\("XXXX"\)|<operator>|<feature>|<your-kerberos>|<op>params|<OP>|<TIER>|<COMPONENT>|<…>'

    # Guidance blockquotes are full of placeholders by design, and get their own
    # warning below, so only look at the document's own lines.
    hits=$(grep -nE "${placeholders}" "${file}" |
        grep -v '^[0-9][0-9]*:>' | cut -d: -f1 | tr '\n' ' ')
    if [ -n "${hits}" ]; then
        err "${file}" "unfilled template placeholder on line(s) ${hits% }"
    fi

    if grep -q '^> ' "${file}"; then
        warn "${file}" "template guidance blockquotes are still present; delete them before opening the PR"
    fi
}

check_document() {
    file="$1"
    rel="${file#"${REPO_ROOT}"/}"

    if ! printf '%s' "$(basename "${file}")" | grep -Eq '^rhwa-[0-9]+\.md$'; then
        err "${rel}" "test case documents are named rhwa-<jira-id>.md, lowercase"
    fi

    check_sections "${rel}"
    check_metadata "${rel}"
    check_leftovers "${rel}"
}

cd "${REPO_ROOT}" || exit 1

files=""
if [ "$#" -gt 0 ]; then
    files="$*"
else
    for candidate in docs/TESTCASES/TEMPLATE.md docs/TESTCASES/rhwa-*.md; do
        [ -e "${candidate}" ] || continue
        files="${files} ${candidate}"
    done
fi

checked=0
for file in ${files}; do
    if [ ! -f "${file}" ]; then
        err "${file}" "no such file"
        continue
    fi

    checked=$((checked + 1))

    # The skeleton is all placeholders and guidance by design; only its shape is
    # worth checking, so that `make new-testcase` keeps producing a valid start.
    if [ "$(basename "${file}")" = "TEMPLATE.md" ]; then
        check_sections "${file}"
        continue
    fi

    check_document "${file}"
done

printf '\nchecked %d file(s): %d error(s), %d warning(s)\n' "${checked}" "${errors}" "${warnings}"

if [ "${errors}" -gt 0 ]; then
    printf 'requirements and review checklist: docs/TESTCASES/AGENTS.md\n'
    exit 1
fi

exit 0
