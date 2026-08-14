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
  overlays=()
  while IFS= read -r line; do
    overlays+=("$line")
  done < <(yq -r "${source_key}.overlays[].location" "$workflow")

  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  step=0
  current="$schema"

  # `speakeasy overlay apply --overlay` is a string, not a slice: passing several
  # flags keeps only the last one, silently. Each overlay needs its own pass.
  for overlay in "${overlays[@]}"; do
    step=$((step + 1))
    speakeasy overlay apply --schema "$current" --overlay "$overlay" >"${work}/${step}.yaml"
    current="${work}/${step}.yaml"
  done

  # This function reproduces the Gram-Internal pipeline by hand, so a
  # transformation it does not know about would make it compare the wrong file.
  while IFS= read -r transformation; do
    step=$((step + 1))
    case "$transformation" in
      removeUnused)
        speakeasy openapi transform remove-unused --schema "$current" --out "${work}/${step}.yaml"
        ;;
      *)
        echo "gen:sdk --check cannot reproduce the '${transformation}' transformation." >&2
        exit 1
        ;;
    esac
    current="${work}/${step}.yaml"
  done < <(yq -r "${source_key}.transformations[]? | keys | .[]" "$workflow")

  if ! diff -q "$current" "$output" >/dev/null 2>&1; then
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
