#!/usr/bin/env bash

#MISE description="Generate SDK from OpenAPI spec"

#USAGE flag "-c --check" help="Check if the Gram-Internal OpenAPI output is up-to-date"
#USAGE flag "-s --skip-versioning" help="Skip automatic SDK version increments"
#USAGE flag "-u --skip-upload-spec" help="Skip uploading the spec to the Speakeasy registry"

set -e

generate() {
  args=()
  if [[ "${usage_skip_versioning:-}" == "true" ]]; then
    args+=(--skip-versioning)
  fi
  if [[ "${usage_skip_upload_spec:-}" == "true" ]]; then
    args+=(--skip-upload-spec)
  fi
  speakeasy run "${args[@]}"
}

check_inputs() {
  workflow=".speakeasy/workflow.yaml"
  source_key=".sources.Gram-Internal"
  schema=$(yq "${source_key}.inputs[0].location" "$workflow")
  output=$(yq "${source_key}.output" "$workflow")
  overlays=()
  while IFS= read -r line; do
    overlays+=("$line")
  done < <(yq -r "${source_key}.overlays[].location" "$workflow")

  args=(--schema "$schema")
  for overlay in "${overlays[@]}"; do
    args+=(--overlay "$overlay")
  done
  result=$(speakeasy overlay apply "${args[@]}")

  if ! diff -q <(echo "$result") "$output" >/dev/null 2>&1; then
    echo "Gram-Internal OpenAPI spec is out of date. Run 'mise gen:sdk' to regenerate." >&2
    exit 1
  fi
  echo "Gram-Internal OpenAPI spec is up to date."
}

if [[ "${usage_check:-}" == "true" ]]; then
  check_inputs
else
  generate
fi
