#!/usr/bin/env bash

#MISE description="Upload a JUnit test report to Datadog Test Optimization"
#MISE dir="{{ config_root }}"
#MISE hide=true

#USAGE flag "--service <service>" required=#true help="Datadog service the tests belong to, e.g. gram-server"
#USAGE flag "--tags <tags>" help="Comma-separated key:value tags added to every test in the report, e.g. shard:1,race:true"
#USAGE flag "--dry-run" help="Parse the report and print what would be sent without uploading it"
#USAGE arg "<report>" help="JUnit XML file, or a directory of them"

set -e -o pipefail

if [[ -z "${DATADOG_API_KEY:-}" ]]; then
  echo "Error: DATADOG_API_KEY environment variable is not set" >&2
  exit 1
fi

args=(junit upload --service "${usage_service:?}")

if [[ -n "${usage_tags:-}" ]]; then
  IFS=, read -r -a tags <<< "$usage_tags"
  for tag in "${tags[@]}"; do
    args+=(--tags "$tag")
  done
fi

if [[ "${usage_dry_run:-false}" == "true" ]]; then
  args+=(--dry-run)
fi

exec aube exec datadog-ci -- "${args[@]}" "${usage_report:?}"
