#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST="${SCRIPT_DIR}/manifest.json"
IDS_FILE=""
OUTPUT=""
BASE_URL="${LOOPS_BASE_URL:-https://app.loops.so/api/v1}"
RESOLVE_ONLY=false
VALIDATE_ONLY=false

usage() {
    cat <<'EOF'
Usage: sync.sh [options]

Options:
  --manifest <path>  Email template manifest (default: manifest.json beside this script)
  --ids <path>       Existing logical-key to Loops-ID JSON mapping
  --output <path>    Destination for the resolved JSON mapping
  --base-url <url>   Loops Content API base URL
  --resolve-only     Resolve existing template IDs without modifying Loops
  --validate-only    Validate manifest inputs without calling Loops
  -h, --help         Show this help
EOF
}

while (($# > 0)); do
    case "$1" in
        --manifest) MANIFEST="${2:?--manifest requires a path}"; shift 2 ;;
        --ids) IDS_FILE="${2:?--ids requires a path}"; shift 2 ;;
        --output) OUTPUT="${2:?--output requires a path}"; shift 2 ;;
        --base-url) BASE_URL="${2:?--base-url requires a URL}"; shift 2 ;;
        --resolve-only) RESOLVE_ONLY=true; shift ;;
        --validate-only) VALIDATE_ONLY=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
done

BASE_URL="${BASE_URL%/}"
MANIFEST_DIR="$(cd "$(dirname "$MANIFEST")" && pwd)"
MANIFEST="${MANIFEST_DIR}/$(basename "$MANIFEST")"

if ! jq -e '
    .version == 1 and
    (.defaults.from_name | type == "string" and length > 0) and
    (.defaults.from_email | type == "string" and length > 0) and
    (.defaults.reply_to_email | type == "string" and length > 0) and
    (.templates | type == "object" and length > 0) and
    all(.templates | to_entries[];
        (.key | type == "string" and length > 0) and
        (.value | type == "object") and
        (.value.managed_name | type == "string" and length > 0) and
        (.value.subject | type == "string" and length > 0) and
        (.value.preview_text | type == "string" and length > 0) and
        (.value.source | type == "string" and length > 0) and
        (.value.variables | type == "array" and all(.[]; type == "string" and length > 0))
    )
' "$MANIFEST" >/dev/null; then
    echo "Invalid email template manifest: $MANIFEST" >&2
    exit 1
fi

if ! TEMPLATE_ROWS="$(jq -er '.templates | to_entries[] | [.key, .value.managed_name, .value.source] | @tsv' "$MANIFEST")"; then
    echo "Could not read templates from manifest: $MANIFEST" >&2
    exit 1
fi
while IFS=$'\t' read -r key managed_name source; do
    if [[ "$managed_name" != "gram.transactional.v2.${key}" ]]; then
        echo "Template $key has unexpected managed name: $managed_name" >&2
        exit 1
    fi
    if [[ -z "$source" || "$source" == */* || "$source" != *.lmx || ! -f "${MANIFEST_DIR}/${source}" ]]; then
        echo "Template $key has invalid LMX source: $source" >&2
        exit 1
    fi
done <<<"$TEMPLATE_ROWS"

TEMPLATE_COUNT="$(jq '.templates | length' "$MANIFEST")"
if [[ "$VALIDATE_ONLY" == true ]]; then
    echo "validated ${TEMPLATE_COUNT} transactional email templates"
    exit 0
fi

if [[ -z "$OUTPUT" ]]; then
    echo "--output is required" >&2
    exit 2
fi
if [[ -z "${LOOPS_API_KEY:-}" || "${LOOPS_API_KEY}" == "unset" ]]; then
    echo "LOOPS_API_KEY is required" >&2
    exit 1
fi

WORK_DIR="$(mktemp -d)"
OUTPUT_TEMP=""
trap 'rm -rf "$WORK_DIR"; [[ -z "$OUTPUT_TEMP" ]] || rm -f "$OUTPUT_TEMP"' EXIT
printf 'Authorization: Bearer %s\nContent-Type: application/json\n' "$LOOPS_API_KEY" > "${WORK_DIR}/headers"
chmod 600 "${WORK_DIR}/headers"
unset LOOPS_API_KEY
MUTATION_COUNT=0
RESPONSE=""

request() {
    local method="$1"
    local path="$2"
    local expected_status="$3"
    local payload="${4:-}"
    local retryable="${5:-false}"
    local response_file="${WORK_DIR}/response"
    local payload_file="${WORK_DIR}/payload"
    local status attempt delay message

    for attempt in 1 2 3; do
        if [[ "$method" != GET ]]; then
            if ((MUTATION_COUNT > 0)); then
                sleep 3
            fi
            MUTATION_COUNT=$((MUTATION_COUNT + 1))
        fi

        local curl_args=(
            --silent
            --show-error
            --connect-timeout 10
            --max-time 60
            --request "$method"
            --header "@${WORK_DIR}/headers"
            --output "$response_file"
            --write-out '%{http_code}'
        )
        if [[ -n "$payload" ]]; then
            printf '%s' "$payload" > "$payload_file"
            curl_args+=(--data-binary "@${payload_file}")
        fi

        if ! status="$(curl "${curl_args[@]}" "${BASE_URL}${path}")"; then
            echo "Loops Content API request failed: $method $path" >&2
            return 1
        fi
        RESPONSE="$(cat "$response_file")"
        if [[ "$status" == "$expected_status" ]]; then
            return 0
        fi

        if [[ "$retryable" == true && "$attempt" -lt 3 && ("$status" == 429 || "$status" -ge 500) ]]; then
            if [[ "$status" == 429 ]]; then
                delay=30
            else
                delay="$attempt"
            fi
            sleep "$delay"
            continue
        fi

        message="$(jq -r '.message // empty' "$response_file" 2>/dev/null || true)"
        echo "Loops Content API $method $path returned HTTP $status${message:+: $message}" >&2
        return 1
    done
}

urlencode() {
    jq -rn --arg value "$1" '$value | @uri'
}

write_output() {
    local value="$1"
    local output_dir
    if [[ -d "$OUTPUT" ]]; then
        echo "Output path is a directory: $OUTPUT" >&2
        return 1
    fi
    output_dir="$(dirname "$OUTPUT")"
    if [[ ! -d "$output_dir" ]]; then
        echo "Output directory does not exist: $output_dir" >&2
        return 1
    fi
    OUTPUT_TEMP="$(mktemp "${output_dir}/.email-template-ids.XXXXXX")"
    printf '%s\n' "$value" | jq --sort-keys . > "$OUTPUT_TEMP"
    mv "$OUTPUT_TEMP" "$OUTPUT"
    OUTPUT_TEMP=""
}

all_templates='[]'
cursor=""
while :; do
    path="/transactional-emails?perPage=50"
    if [[ -n "$cursor" ]]; then
        path+="&cursor=$(urlencode "$cursor")"
    fi
    request GET "$path" 200 "" true
    if ! jq -e '.data | type == "array"' <<<"$RESPONSE" >/dev/null; then
        echo "Loops returned an invalid transactional email list" >&2
        exit 1
    fi
    all_templates="$(jq -cn --argjson current "$all_templates" --argjson page "$RESPONSE" '$current + $page.data')"
    cursor="$(jq -r '.pagination.nextCursor // empty' <<<"$RESPONSE")"
    [[ -n "$cursor" ]] || break
done

existing_ids='{}'
if [[ -n "$IDS_FILE" ]]; then
    if ! existing_ids="$(jq -ce 'if type == "object" then . else error("expected an object") end' "$IDS_FILE")"; then
        echo "Invalid template ID mapping: $IDS_FILE" >&2
        exit 1
    fi
fi
resolved='{}'

while IFS= read -r key; do
    spec="$(jq -c --arg key "$key" '.templates[$key]' "$MANIFEST")"
    managed_name="$(jq -r '.managed_name' <<<"$spec")"
    matches="$(jq -c --arg name "$managed_name" '[.[] | select(.name == $name)]' <<<"$all_templates")"
    match_count="$(jq 'length' <<<"$matches")"

    if [[ "$RESOLVE_ONLY" == true ]]; then
        if [[ "$match_count" -ne 1 ]]; then
            echo "Resolve template $key: found $match_count Loops emails named $managed_name" >&2
            exit 1
        fi
        transactional="$(jq -c '.[0]' <<<"$matches")"
    else
        mapped_id="$(jq -r --arg key "$key" '.[$key] // empty' <<<"$existing_ids")"
        if [[ -n "$mapped_id" ]]; then
            request GET "/transactional-emails/$(urlencode "$mapped_id")" 200 "" true
            transactional="$RESPONSE"
            actual_name="$(jq -r '.name // empty' <<<"$transactional")"
            if [[ "$actual_name" != "$managed_name" ]]; then
                echo "Mapped template $key belongs to $actual_name, expected $managed_name" >&2
                exit 1
            fi
        elif [[ "$match_count" -eq 1 ]]; then
            transactional="$(jq -c '.[0]' <<<"$matches")"
        elif [[ "$match_count" -eq 0 ]]; then
            payload="$(jq -cn --arg name "$managed_name" '{name: $name}')"
            request POST /transactional-emails 201 "$payload" false
            transactional="$RESPONSE"
        else
            echo "Resolve template $key: found $match_count Loops emails named $managed_name" >&2
            exit 1
        fi
    fi

    transactional_id="$(jq -er '.id | strings | select(length > 0)' <<<"$transactional")"
    resolved="$(jq -c --arg key "$key" --arg id "$transactional_id" '. + {($key): $id}' <<<"$resolved")"

    if [[ "$RESOLVE_ONLY" == true ]]; then
        continue
    fi

    request POST "/transactional-emails/$(urlencode "$transactional_id")/draft" 200 "" false
    draft_id="$(jq -er '.draftEmailMessageId | strings | select(length > 0)' <<<"$RESPONSE")"
    request GET "/email-messages/$(urlencode "$draft_id")" 200 "" true
    revision_id="$(jq -er '.contentRevisionId | strings | select(length > 0)' <<<"$RESPONSE")"

    source="$(jq -r '.source' <<<"$spec")"
    payload="$(jq -cn \
        --arg expectedRevisionId "$revision_id" \
        --arg subject "$(jq -r '.subject' <<<"$spec")" \
        --arg previewText "$(jq -r '.preview_text' <<<"$spec")" \
        --arg fromName "$(jq -r '.defaults.from_name' "$MANIFEST")" \
        --arg fromEmail "$(jq -r '.defaults.from_email' "$MANIFEST")" \
        --arg replyToEmail "$(jq -r '.defaults.reply_to_email' "$MANIFEST")" \
        --rawfile lmx "${MANIFEST_DIR}/${source}" \
        '{
            expectedRevisionId: $expectedRevisionId,
            subject: $subject,
            previewText: $previewText,
            fromName: $fromName,
            fromEmail: $fromEmail,
            replyToEmail: $replyToEmail,
            emailFormat: "styled",
            lmx: ($lmx | gsub("^\\s+|\\s+$"; ""))
        }')"
    request POST "/email-messages/$(urlencode "$draft_id")" 200 "$payload" true
    request GET "/email-messages/$(urlencode "$draft_id")/guardian" 200 "" true
    if [[ "$(jq '.errors | length' <<<"$RESPONSE")" -gt 0 ]]; then
        echo "Guardian rejected template $key:" >&2
        jq -r '.errors[] | "  \(.rule): \(.description)"' <<<"$RESPONSE" >&2
        exit 1
    fi
    request POST "/transactional-emails/$(urlencode "$transactional_id")/publish" 200 "" false
    echo "$key: published"
done < <(jq -r '.templates | keys[]' "$MANIFEST")

write_output "$resolved"
