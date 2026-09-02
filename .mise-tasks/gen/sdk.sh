#!/usr/bin/env bash

#MISE description="Generate SDK from OpenAPI spec"

#USAGE flag "-c --check" help="Check if the Gram-Internal OpenAPI output is up-to-date"

set -e

# The Speakeasy CLI version is baked into every generated artifact (gen.lock,
# workflow.lock, the SDK_METADATA userAgent), so generating with anything other
# than the pinned version produces churn CI rejects. `speakeasy run` re-execs
# into the version named by speakeasyVersion in .speakeasy/workflow.yaml, so a
# system-wide install (Homebrew, `go install`) ahead of the mise shim on PATH
# still generates with the pinned version.
generate() {
  # CI=true keeps `speakeasy run` non-interactive. It used to also matter
  # because the TypeScript target compiled the SDK by invoking pnpm, which
  # prompts to purge node_modules and aborts without a TTY; since the SDK was
  # inlined, gen.yaml sets compileCommand to `true` and no package manager is
  # invoked at all.
  CI=true speakeasy run --skip-versioning --skip-upload-spec --minimal
}

check_inputs() {
  workflow=".speakeasy/workflow.yaml"
  source_key=".sources.Gram-Internal"
  schema=$(yq "${source_key}.inputs[0].location" "$workflow")
  output=$(yq "${source_key}.output" "$workflow")
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT

  current="$schema"
  overlay_index=0
  while IFS= read -r overlay; do
    overlay_index=$((overlay_index + 1))
    next="$tmpdir/overlay-${overlay_index}.yaml"
    speakeasy overlay apply --schema "$current" --overlay "$overlay" --out "$next" >/dev/null 2>&1
    current="$next"
  done < <(yq -r "${source_key}.overlays[].location" "$workflow")

  result="$current"
  if [[ "$(yq "${source_key}.transformations[0].removeUnused // false" "$workflow")" == "true" ]]; then
    result="$tmpdir/normalized.yaml"
    speakeasy openapi transform remove-unused --schema "$current" --out "$result" >/dev/null 2>&1
  fi

  if ! diff -q "$result" "$output" >/dev/null 2>&1; then
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
