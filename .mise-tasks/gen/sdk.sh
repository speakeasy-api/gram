#!/usr/bin/env bash

#MISE description="Generate SDK from OpenAPI spec"

#USAGE flag "-c --check" help="Check if the Gram-Internal and Gram-Admin OpenAPI outputs are up-to-date"

set -e

# The SDK generator is installed as speakeasy-sdks (see mise.toml). mise only
# applies rename_exe on install, so an install made before the rename still
# serves the old speakeasy name. Reinstall it once in that case.
if mise which speakeasy >/dev/null 2>&1; then
  mise install --force "github:speakeasy-api/speakeasy" >&2
fi

# The Speakeasy CLI version is baked into every generated artifact (gen.lock,
# workflow.lock, the SDK_METADATA userAgent), so generating with anything other
# than the pinned version produces churn CI rejects. `speakeasy-sdks run` re-execs
# into the version named by speakeasyVersion in .speakeasy/workflow.yaml, so a
# system-wide install (Homebrew, `go install`) ahead of the mise shim on PATH
# still generates with the pinned version.
generate() {
  # CI=true keeps `speakeasy-sdks run` non-interactive. It used to also matter
  # because the TypeScript target compiled the SDK by invoking pnpm, which
  # prompts to purge node_modules and aborts without a TTY; since the SDK was
  # inlined, gen.yaml sets compileCommand to `true` and no package manager is
  # invoked at all.
  CI=true speakeasy-sdks run "$@" --skip-versioning --skip-upload-spec --minimal
}

check_inputs() {
  workflow=".speakeasy/workflow.yaml"
  tmpdir=$(mktemp -d)
  sources=(Gram-Internal Gram-Admin)
  outputs=()

  for source in "${sources[@]}"; do
    output=$(yq ".sources[\"${source}\"].output" "$workflow")
    outputs+=("$output")
    cp "$output" "$tmpdir/$source.yaml"
  done

  restore_inputs() {
    for i in "${!sources[@]}"; do
      cp "$tmpdir/${sources[$i]}.yaml" "${outputs[$i]}"
    done
    rm -rf "$tmpdir"
  }
  trap restore_inputs EXIT

  status=0
  for i in "${!sources[@]}"; do
    source=${sources[$i]}
    output=${outputs[$i]}
    generate --source "$source" >/dev/null 2>&1

    if ! diff -q "$tmpdir/$source.yaml" "$output" >/dev/null 2>&1; then
      echo "${source} OpenAPI spec is out of date. Run 'mise gen:sdk' to regenerate." >&2
      status=1
    else
      echo "${source} OpenAPI spec is up to date."
    fi
  done

  return "$status"
}

if [[ "${usage_check:-}" == "true" ]]; then
  check_inputs
else
  generate
fi
