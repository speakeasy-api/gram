#!/usr/bin/env bash

#MISE description="Insert bursts of synthetic hook events into local ClickHouse for demoing the onboarding wizard's confirm-traffic step"
#MISE dir="{{ config_root }}/server"

#USAGE flag "-p --project-id <id>" help="Target project UUID. Skips org lookup."
#USAGE flag "-o --org-slug <slug>" help="Org slug; uses the first project of that org."
#USAGE flag "-b --burst-size <n>" default="5" help="Events per burst."
#USAGE flag "-c --bursts <n>" default="20" help="Number of bursts (0 for infinite)."
#USAGE flag "-i --interval <duration>" default="800ms" help="Wait between bursts (Go duration, e.g. 500ms, 2s)."
#USAGE flag "--block-rate <rate>" default="0.1" help="Fraction of events (0-1) marked blocked."

set -e

args=()
if [ -n "${usage_project_id:-}" ]; then
  args+=("-project-id" "${usage_project_id}")
fi
if [ -n "${usage_org_slug:-}" ]; then
  args+=("-org-slug" "${usage_org_slug}")
fi
args+=(
  "-burst-size" "${usage_burst_size:-5}"
  "-bursts" "${usage_bursts:-20}"
  "-interval" "${usage_interval:-800ms}"
  "-block-rate" "${usage_block_rate:-0.1}"
)

exec go run ./cmd/seedhooks "${args[@]}"
