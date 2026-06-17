#!/usr/bin/env bash

GINKGO="${GINKGO:-ginkgo}"
GOPATH="${GOPATH:-${HOME}/go}"
PATH=$PATH:$GOPATH/bin
TEST_DIR="./tests"

# In CI, ARTIFACT_DIR is set by ci-operator and is collected/uploaded automatically.
# Fall back to ECO_REPORTS_DUMP_DIR if already set, then /tmp/reports for local runs.
export ECO_REPORTS_DUMP_DIR="${ARTIFACT_DIR:-${ECO_REPORTS_DUMP_DIR:-/tmp/reports}}"

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

# Copy reportxml testrun XML to SHARED_DIR for the Polarion reporter post step.
COPY_EXIT=0
if [[ -n "${SHARED_DIR}" ]]; then
  if ! find "${ECO_REPORTS_DUMP_DIR}" -name '*_testrun.xml' -exec cp -t "${SHARED_DIR}/" {} +; then
    echo "Failed to copy *_testrun.xml from ${ECO_REPORTS_DUMP_DIR} to ${SHARED_DIR}" >&2
    COPY_EXIT=1
  fi
fi

if [[ $GINKGO_EXIT -eq 0 && $COPY_EXIT -ne 0 ]]; then
  exit $COPY_EXIT
fi
exit $GINKGO_EXIT
