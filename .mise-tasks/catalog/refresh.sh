#!/usr/bin/env bash

#MISE description="Preview a source catalog refresh, or apply an explicitly reviewed plan"
#MISE dir="{{ config_root }}"
#USAGE flag "--action <action>" default="preview" help="Preview changes or apply a reviewed plan" { choices "preview" "apply" }
#USAGE flag "--plan-id <plan-id>" help="Exact UUID returned by the preview; required for apply"

set -euo pipefail

action="${usage_action:-preview}"
plan_id="${usage_plan_id:-}"
case "$action" in
    preview)
        if [ -n "$plan_id" ]; then
            echo "--plan-id is only valid with --action apply." >&2
            exit 1
        fi
        exec bash "$(dirname "$0")/transfer.sh" --action preview
        ;;
    apply)
        if ! [[ "$plan_id" =~ ^[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}$ ]]; then
            echo "Apply requires --plan-id with the exact UUID from a reviewed preview. Run mise run catalog:refresh first." >&2
            exit 1
        fi
        exec bash "$(dirname "$0")/transfer.sh" --action apply --plan-id "$plan_id"
        ;;
    *)
        echo "--action must be preview or apply." >&2
        exit 1
        ;;
esac
