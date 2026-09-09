#!/usr/bin/env bash
# Read-only registry inspection; extracted metadata and exact inputs are retained.
set -euo pipefail
if [[ $# != 4 ]]; then
    echo "usage: $0 BUNDLE EXPECTED_OPERATOR_IMAGE EXPECTED_VERSION REPORT_DIR" >&2
    exit 2
fi
bundle=$1 expected_image=$2 expected_version=$3 report_dir=$4
: "${YQ:?Set YQ to the pinned NHC bin/yq}"
mkdir -p "$report_dir/manifests" "$report_dir/metadata"
# Resolve before extraction so a movable input tag cannot change halfway through.
oc image info "$bundle" -o json > "$report_dir/bundle-image.json"
bundle_digest=$("$YQ" -r '.digest' "$report_dir/bundle-image.json")
[[ $bundle_digest =~ ^sha256:[a-f0-9]{64}$ ]]
bundle_repo=${bundle%@*}
if [[ ${bundle_repo##*/} == *:* ]]; then bundle_repo=${bundle_repo%:*}; fi
bundle="$bundle_repo@$bundle_digest"
oc image extract "$bundle" --path "/manifests/:$report_dir/manifests" --path "/metadata/:$report_dir/metadata" --confirm
mapfile -t csvs < <(find "$report_dir/manifests" -name '*clusterserviceversion.yaml')
[[ ${#csvs[@]} == 1 ]] || { echo "Expected exactly one candidate CSV" >&2; exit 1; }
csv=${csvs[0]}
[[ $("$YQ" -r '.annotations."operators.operatorframework.io.bundle.package.v1"' "$report_dir/metadata/annotations.yaml") == "${NHC_INSPECT_PACKAGE:-node-healthcheck-operator}" ]]
[[ $("$YQ" -r '.spec.version' "$csv") == "$expected_version" ]]
mapfile -t managers < <("$YQ" -r '.spec.install.spec.deployments[].spec.template.spec.containers[] | select(.name == "manager") | .image' "$csv")
[[ ${#managers[@]} == 1 && -n ${managers[0]} ]]
manager_image=${managers[0]}
[[ $("$YQ" -r '.metadata.annotations.containerImage' "$csv") == "$manager_image" ]]
oc image info "$expected_image" -o json > "$report_dir/expected-operator-image.json"
oc image info "$manager_image" -o json > "$report_dir/manager-image.json"
expected_digest=$("$YQ" -r '.digest' "$report_dir/expected-operator-image.json")
actual_digest=$("$YQ" -r '.digest' "$report_dir/manager-image.json")
[[ $expected_digest =~ ^sha256:[a-f0-9]{64}$ && $actual_digest == "$expected_digest" ]] || {
    echo "Bundle manager does not match independently supplied candidate operator" >&2; exit 1;
}
# Record every deployment/init/related/env image, including resolved floating
# related references. Resolving a tag here does not freeze that tag on nodes.
{
    "$YQ" -r '.spec.install.spec.deployments[].spec.template.spec.containers[].image' "$csv"
    "$YQ" -r '.spec.install.spec.deployments[].spec.template.spec.initContainers[]?.image' "$csv"
    "$YQ" -r '.spec.relatedImages[]?.image' "$csv"
    "$YQ" -r '.spec.install.spec.deployments[].spec.template.spec.containers[].env[]? | select(.name | test("^RELATED_IMAGE_")) | .value' "$csv"
} | sort -u | sed '/^null$/d; /^$/d' > "$report_dir/images.txt"
index=0
while IFS= read -r image; do
    oc image info "$image" -o json > "$report_dir/related-$index.json"
    index=$((index + 1))
done < "$report_dir/images.txt"
"$YQ" '{"name": .metadata.name, "version": .spec.version, "replaces": .spec.replaces, "skipRange": .metadata.annotations."olm.skipRange", "relatedImages": .spec.relatedImages}' "$csv" > "$report_dir/upgrade-metadata.yaml"
# %q makes this generated file safe to source, including arbitrary registry text.
printf 'export NHC_UPGRADE_CANDIDATE_IMAGE=%q\n' "$manager_image" > "$report_dir/verified-image.env"
printf 'export NHC_UPGRADE_CANDIDATE_BUNDLE=%q\n' "$bundle" >> "$report_dir/verified-image.env"
printf 'export NHC_UPGRADE_CANDIDATE_VERSION=%q\n' "$expected_version" >> "$report_dir/verified-image.env"
cat "$report_dir/upgrade-metadata.yaml"
