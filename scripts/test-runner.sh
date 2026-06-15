#!/usr/bin/env bash

GINKGO="${GINKGO:-ginkgo}"
TEST_DIR="./tests"

# In CI, write JUnit XML to ARTIFACT_DIR so Prow picks it up; fall back to /tmp/reports locally.
export ECO_REPORTS_DUMP_DIR="${ECO_REPORTS_DUMP_DIR:-${ARTIFACT_DIR:-/tmp/reports}}"

# Check that ECO_TEST_FEATURES environment variable has been set
if [[ -z "${ECO_TEST_FEATURES}" ]]; then
    echo "ECO_TEST_FEATURES environment variable is undefined"
    exit 1
fi

# Set feature_dirs to top-level test directory when "all" feature provided
if [[ "${ECO_TEST_FEATURES}" == "all" ]]; then
    feature_dirs=${TEST_DIR}
else
    # Find all test directories matching provided features
    for feature in ${ECO_TEST_FEATURES}; do
        discovered_features=$(find $TEST_DIR -depth -name "${feature}" -not -path '*/internal/*' 2> /dev/null)
        if [[ ! -z $discovered_features ]]; then
            feature_dirs+=" "$discovered_features
        else
            if [[ "${ECO_VERBOSE_SCRIPT}" == "true" ]]; then
                echo "Could not find any feature directories matching ${feature}"
            fi
        fi
    done

    if [[ -z "${feature_dirs}" ]]; then
        echo "Could not find any feature directories for provided features: ${ECO_TEST_FEATURES}"
        exit 1
    fi

    if [[ "${ECO_VERBOSE_SCRIPT}" == "true" ]]; then
        echo "Found feature directories:"
        for directory in $feature_dirs; do printf "$directory\n"; done
    fi
fi


# Build ginkgo command
cmd="${GINKGO} -timeout=24h --keep-going --require-suite -r"

if [[ "${ECO_TEST_VERBOSE}" == "true" ]]; then
    cmd+=" -vv"
fi

if [[ "${ECO_TEST_TRACE}" == "true" ]]; then
    cmd+=" --trace"
fi

if [[ ! -z "${ECO_TEST_LABELS}" ]]; then
    cmd+=" --label-filter=\"${ECO_TEST_LABELS}\""
fi
cmd+=" $@ $feature_dirs"   # add user args before feature dirs

# Execute ginkgo command
echo $cmd
eval $cmd
GINKGO_EXIT=$?

# Generate HTML summary for Prow artifacts (no-op locally when ARTIFACT_DIR is unset)
[ -n "${ARTIFACT_DIR}" ] && python3 "$(dirname "$0")/junit-to-html.py" || echo "Warning: HTML report generation failed" >&2

# Copy JUnit XML to SHARED_DIR so post-test steps (e.g. Polarion reporter) can read them.
if [[ -n "${SHARED_DIR}" ]]; then
  find "${ECO_REPORTS_DUMP_DIR}" -name '*_junit.xml' -exec cp -t "${SHARED_DIR}/" {} + 2>/dev/null
fi

exit $GINKGO_EXIT
