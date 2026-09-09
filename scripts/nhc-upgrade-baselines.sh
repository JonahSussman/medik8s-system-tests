#!/usr/bin/env bash
# Resolve and inspect the two upstream prerequisite bundles; no cluster writes.
set -euo pipefail
[[ $# == 1 ]] || { echo "usage: $0 REPORT_DIR" >&2; exit 2; }
report_dir=$1
: "${YQ:?Set YQ to the pinned NHC bin/yq}"
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
mkdir -p "$report_dir"
for baseline in old snr; do
    if [[ $baseline == old ]]; then
        lookup=${NHC_UPGRADE_OLD_BUNDLE:-quay.io/medik8s/node-healthcheck-operator-bundle:v0.12.0}
        package=node-healthcheck-operator
        version=0.12.0
    else
        lookup=${NHC_UPGRADE_SNR_BUNDLE:-quay.io/medik8s/self-node-remediation-operator-bundle:v0.13.0}
        package=self-node-remediation
        version=0.13.0
    fi
    directory="$report_dir/$baseline"
    mkdir -p "$directory/manifests"
    oc image info "$lookup" -o json > "$directory/lookup.json"
    digest=$("$YQ" -r '.digest' "$directory/lookup.json")
    [[ $digest =~ ^sha256:[a-f0-9]{64}$ ]]
    repository=${lookup%@*}
    if [[ ${repository##*/} == *:* ]]; then repository=${repository%:*}; fi
    bundle="$repository@$digest"
    oc image extract "$bundle" --path "/manifests:$directory/manifests" --confirm
    mapfile -t csvs < <(find "$directory/manifests" -name '*clusterserviceversion.yaml')
    [[ ${#csvs[@]} == 1 ]]
    manager=$("$YQ" -r '.spec.install.spec.deployments[].spec.template.spec.containers[] | select(.name == "manager") | .image' "${csvs[0]}")
    NHC_INSPECT_PACKAGE=$package bash "$script_dir/nhc-upgrade-inspect.sh" "$bundle" "$manager" "$version" "$directory"
    if [[ $baseline == old ]]; then
        printf 'export NHC_UPGRADE_OLD_BUNDLE=%q\nexport NHC_UPGRADE_OLD_IMAGE=%q\n' "$bundle" "$manager" > "$report_dir/baseline-inputs.env"
    else
        printf 'export NHC_UPGRADE_SNR_BUNDLE=%q\n' "$bundle" >> "$report_dir/baseline-inputs.env"
    fi
done
