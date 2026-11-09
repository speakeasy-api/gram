#!/usr/bin/env bash

#MISE description="Preview or explicitly confirm a destination-only catalog reset"
#MISE dir="{{ config_root }}"
#USAGE flag "--action <action>" default="preview" help="Preview, apply, or inspect reset status" { choices "preview" "apply" "status" }
#USAGE flag "--plan-id <plan-id>" help="Exact reset preview UUID; required for apply"
#USAGE flag "--confirmation <confirmation>" help="Exact server preview confirmation text; required for apply"

set -euo pipefail

action="${usage_action:-preview}"
plan_id="${usage_plan_id:-}"
confirmation="${usage_confirmation:-}"
case "$action" in
    preview|status)
        if [ -n "$plan_id" ] || [ -n "$confirmation" ]; then
            echo "--plan-id and --confirmation are only valid with --action apply." >&2
            exit 1
        fi
        exec bash "$(dirname "$0")/transfer.sh" --operation reset --action "$action"
        ;;
    apply)
        if ! [[ "$plan_id" =~ ^[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}$ ]] || [ -z "$confirmation" ]; then
            echo "Reset apply requires --plan-id and --confirmation with the exact values from a reviewed reset preview. Run mise run catalog:reset first." >&2
            exit 1
        fi
        exec bash "$(dirname "$0")/transfer.sh" --operation reset --action apply --plan-id "$plan_id" --confirmation "$confirmation"
        ;;
    *) echo "--action must be preview, apply, or status; reset cannot bootstrap." >&2; exit 1 ;;
esac
