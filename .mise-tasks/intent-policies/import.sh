#!/usr/bin/env bash

#MISE description="Import a directory of agent transcripts into the local dev project for intent policy analysis"
#MISE dir="{{ config_root }}"
#USAGE flag "--dir <dir>" required=#true help="Transcript directory with one subdirectory per source"
#USAGE flag "--limit <limit>" default="150" help="Maximum transcripts to import, round-robin across sources"

set -euo pipefail

# Local only: transcripts are written to the dev org's project, never committed.
cd server
exec go run . intent-policies-import --dir "${usage_dir:?}" --limit "${usage_limit:?}"
