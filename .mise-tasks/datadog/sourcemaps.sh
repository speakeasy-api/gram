#!/usr/bin/env bash

#MISE description="Upload client sourcemaps to DataDog"
#MISE dir="{{ config_root }}"
#MISE hide=true

#USAGE flag "--git-sha <sha>" help="The Git SHA to to use as the version tag"

set -e -o pipefail

if [[ -z "${DATADOG_API_KEY:-}" ]]; then
  echo "Error: DATADOG_API_KEY environment variable is not set" >&2
  exit 1
fi

exec datadog-ci sourcemaps upload ./client/dashboard/dist \
  --service gram \
  --release-version "${usage_git_sha:?}" \
  --minified-path-prefix /assets \
  --dry-run