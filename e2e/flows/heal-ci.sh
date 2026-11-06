#!/usr/bin/env bash
# CI only, as root: heal one failing flow with a Claude Code agent inside the
# agent jail (e2e/agent-jail/README.md). Run from the repository root after
# `jail.sh install`, `jail.sh browsers` and `jail.sh start <ports>`.
#
#   sudo --preserve-env=GRAM_SITE_URL,GRAM_SERVER_URL,JAIL_PORTS e2e/flows/heal-ci.sh <flow>
#
# The agent gets a copy of e2e/ and nothing else of the checkout, can reach
# only the bridged stack ports and the Anthropic proxy, and holds no secret.
# Out of its workspace come only: a validated verdict, the transcript, and the
# healed script or failure screenshot, each checked to be a regular file
# inside the workspace.

set -euo pipefail

flow="${1:?flow}"
[[ "$flow" =~ ^[a-z0-9-]+$ ]] || {
    echo "bad flow name" >&2
    exit 2
}
[ -f "e2e/flows/${flow}/story.md" ] || {
    echo "no story for ${flow}" >&2
    exit 2
}
owner="${SUDO_USER:?run through sudo}"
: "${GRAM_SITE_URL:?}" "${GRAM_SERVER_URL:?}" "${JAIL_PORTS:?}"

jail=/opt/agent-jail
state=/var/lib/agent-jail
work="$state/work-${flow}"
out=e2e/.flows

install -m 0755 e2e/flows/jail-bin/* "$jail/bin/"

rm -rf "$work"
install -d -m 0755 "$work" "$work/e2e" "$work/out"
cp tsconfig.strict.json "$work/"
tar -C e2e --exclude=./node_modules --exclude=./.flows --exclude=./.playwright \
    --exclude=./.magellan -cf - . | tar -C "$work/e2e" -xf -
install -d "$work/e2e/.flows"
if [ -f "$out/results.json" ]; then cp "$out/results.json" "$work/e2e/.flows/replay-results.json"; fi
if [ -d "$out/artifacts" ]; then cp -r "$out/artifacts" "$work/e2e/.flows/replay-artifacts"; fi
head -c 2000000 "$out/branch.diff" >"$work/branch.diff" 2>/dev/null || : >"$work/branch.diff"
ln -s "$jail/tools/node_modules" "$work/node_modules"
ln -s "$jail/tools/node_modules" "$work/e2e/node_modules"
cat >"$work/cli.config.json" <<JSON
{
  "browser": {
    "browserName": "chromium",
    "launchOptions": { "headless": true },
    "contextOptions": { "ignoreHTTPSErrors": true, "viewport": { "width": 1440, "height": 900 } }
  },
  "outputDir": "$work/.playwright-cli"
}
JSON
# shellcheck disable=SC2016 # literal backticks: markdown for the prompt
diff_hint='DIFF=The file `branch.diff`'
"$jail/bin/node" e2e/agent-jail/render.mjs e2e/flows/HEAL.md \
    "FLOW=${flow}" \
    "RESULTS=e2e/.flows/replay-results.json" \
    "ARTIFACTS=e2e/.flows/replay-artifacts/" \
    "$diff_hint" \
    "BROWSER=flow-browser" \
    "TEST=flow-test" \
    "TYPECHECK=flow-typecheck" \
    "SITE_URL=${GRAM_SITE_URL}" >"$work/prompt.md"
# This run's own user: no other run, including a parallel heal, can read or
# change this workspace.
run_user="$("$jail/jail.sh" user "heal-${flow}")"
chown -R "$run_user:$run_user" "$work"
chmod 0700 "$work"

env_file="$state/env-${flow}"
{
    printf '%s=%q\n' \
        FLOW "$flow" \
        JAIL_WORK "$work" \
        GRAM_SITE_URL "$GRAM_SITE_URL" \
        GRAM_SERVER_URL "$GRAM_SERVER_URL" \
        PLAYWRIGHT_BROWSERS_PATH "$jail/browsers" \
        ANTHROPIC_BASE_URL "http://127.0.0.1:${JAIL_PROXY_PORT:-3128}" \
        ANTHROPIC_API_KEY "jail-placeholder-the-proxy-adds-the-key" \
        CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC 1 \
        DISABLE_AUTOUPDATER 1 \
        DISABLE_TELEMETRY 1 \
        DISABLE_ERROR_REPORTING 1 \
        CI 1
} >"$env_file"
chmod 0644 "$env_file"

# shellcheck disable=SC2016 # expanded inside the jail
agent='cd "$JAIL_WORK" && claude -p --output-format json \
    --tools "Bash,Read,Write,Edit,Glob,Grep" \
    --allowedTools "Read,Glob,Grep,Edit(./e2e/flows/$FLOW/flow.spec.ts),Write(./e2e/flows/$FLOW/flow.spec.ts),Edit(./e2e/.flows/**),Write(./e2e/.flows/**),Bash(flow-browser:*),Bash(flow-test),Bash(flow-typecheck)" \
    --max-turns 80 <prompt.md >out/claude.json 2>out/claude.err'
status=0
timeout 25m "$jail/jail.sh" run "heal-${flow}" "$env_file" "$JAIL_PORTS" bash -c "$agent" || status=$?
echo "heal ${flow}: agent exited with ${status}"
# Enough to see why a run failed. The jail holds no secret and the healer sees
# only the throwaway local stack, so its errors are safe in a public log.
if [ -f "$work/out/claude.json" ] && [ ! -L "$work/out/claude.json" ]; then
    head -c 4000000 "$work/out/claude.json" |
        jq -c '{type, subtype, is_error, num_turns, duration_ms}' 2>/dev/null || echo "no result JSON"
fi
if [ -f "$work/out/claude.err" ] && [ ! -L "$work/out/claude.err" ]; then
    head -n 20 "$work/out/claude.err" | cut -c1-300
fi

# Everything below reads files the agent wrote. The jail has exited, so no
# agent process can swap a file between the check and the copy.
inside() {
    local real
    real="$(readlink -f "$1")" || return 1
    case "$real" in "$work"/*) [ -f "$real" ] && [ ! -L "$1" ] ;; *) return 1 ;; esac
}

install -d -o "$owner" "$out" "$out/healed"
result=""
if inside "$work/out/claude.json"; then
    result="$(head -c 4000000 "$work/out/claude.json" | jq -r '.result // empty' 2>/dev/null || true)"
    printf '%s\n' "$result" | head -c 1000000 >"$out/${flow}-transcript.txt"
fi
if inside "$work/out/claude.err"; then
    head -c 200000 "$work/out/claude.err" >"$out/${flow}-agent-stderr.txt"
fi
verdict="$(printf '%s\n' "$result" | tail -n 1 | jq -c --arg flow "$flow" '
    select(type == "object" and (.verdict | IN("healed", "broken", "changed")))
    | {flow: $flow, verdict,
       step: ((.step // "") | tostring | .[0:200]),
       summary: ((.summary // "") | tostring | .[0:400])}' 2>/dev/null || true)"
if [ -z "$verdict" ]; then
    verdict="$(jq -nc --arg flow "$flow" '{flow: $flow, verdict: "unknown", step: "", summary: "the agent did not return a verdict"}')"
fi
printf '%s\n' "$verdict" >"$out/${flow}-verdict.json"

spec="$work/e2e/flows/${flow}/flow.spec.ts"
if [ "$(jq -r .verdict <<<"$verdict")" = healed ] && inside "$spec" &&
    [ "$(stat -c %s "$spec")" -lt 262144 ]; then
    install -D -m 0644 "$spec" "$out/healed/${flow}/flow.spec.ts"
fi
png="$work/e2e/.flows/${flow}-broken.png"
if inside "$png" && [ "$(stat -c %s "$png")" -lt 10485760 ] &&
    [ "$(head -c 8 "$png" | od -An -tx1 | tr -d ' \n')" = 89504e470d0a1a0a ]; then
    install -m 0644 "$png" "$out/${flow}-broken.png"
fi
chown -R "$owner" "$out"
