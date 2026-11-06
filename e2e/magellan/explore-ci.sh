#!/usr/bin/env bash
# CI only, as root: run a prepared Magellan workspace inside the agent jail
# (e2e/agent-jail/README.md). Called by `mise run magellan:explore` in remote
# mode, after `jail.sh install`, `jail.sh browsers` and `jail.sh start` (with
# JAIL_ALLOWED_HOSTS set to the site's host).
#
#   sudo --preserve-env=MAGELLAN_SESSION e2e/magellan/explore-ci.sh \
#       <workspace> <site-url> <org> <budget-minutes> <tools> <allowed-tools>
#
# The bot's session goes into the jailed browser before the agent starts and
# its file is deleted before the agent runs. The agent's browser reaches only
# the site through the proxy, the agent reaches only the Anthropic proxy, and
# it holds no secret. Out of the jail come only findings.jsonl, notes.md and
# PNG screenshots, each checked to be a regular file inside the workspace.

set -euo pipefail

ws="${1:?workspace}" site_url="${2:?site url}" org="${3:?org}" budget="${4:?budget}"
tools="${5:?tools}" allowed="${6:?allowed tools}"
: "${MAGELLAN_SESSION:?MAGELLAN_SESSION is required}"
owner="${SUDO_USER:?run through sudo}"
case "$ws" in e2e/.magellan/*) ;; *)
    echo "workspace must be under e2e/.magellan" >&2
    exit 2
    ;;
esac
[[ "$budget" =~ ^[0-9]+$ ]] || exit 2
[[ "$org" =~ ^[A-Za-z0-9_-]+$ ]] || exit 2

jail=/opt/agent-jail
state=/var/lib/agent-jail
work="$state/work-magellan"
secret="$state/magellan-secret"

# Root owns the workspace; the agent can write out/ and its home only.
run_user="$("$jail/jail.sh" user magellan)"
rm -rf "$work" "$secret"
install -d -m 0750 -o root -g "$run_user" "$work"
install -d -m 0755 "$work/bin"
cp -r "$ws/prompt.md" "$ws/context" "$work/"
install -d -m 0755 -o "$run_user" -g "$run_user" "$work/out" "$work/out/.pw"
install -m 0644 e2e/magellan/browser.mjs "$jail/magellan-browser.mjs"
"$jail/bin/node" -e '
  const [file, site, session, out] = process.argv.slice(1);
  require("node:fs").writeFileSync(file, JSON.stringify({
    siteURL: site, session, outDir: out, backend: ["playwright-cli"], cwd: out + "/.pw",
  }, null, 2));
' "$work/bin/browser.json" "$site_url" magellan "$work/out"
cat >"$work/bin/magellan-browser" <<SH
#!/usr/bin/env bash
exec /opt/agent-jail/bin/node /opt/agent-jail/magellan-browser.mjs "$work/bin/browser.json" "\$@"
SH
chmod 0755 "$work/bin/magellan-browser"
# The browser reaches the site only through the jail's proxy.
cat >"$work/cli.config.json" <<JSON
{
  "browser": {
    "browserName": "chromium",
    "launchOptions": { "headless": true, "proxy": { "server": "http://127.0.0.1:${JAIL_PROXY_PORT:-3128}" } },
    "contextOptions": { "viewport": { "width": 1440, "height": 900 }, "locale": "en-US", "timezoneId": "UTC" }
  },
  "network": { "allowedOrigins": ["${site_url}"] },
  "outputDir": "$work/out/.pw"
}
JSON
chmod -R a+rX "$work/bin" "$work/context" "$work/prompt.md" "$work/cli.config.json"

# The session, as browser state only the jail user can read. Read from the
# environment, so it never appears in a process's arguments.
install -d -m 0700 -o "$run_user" -g "$run_user" "$secret"
MAGELLAN_STATE_FILE="$secret/state.json" MAGELLAN_SITE_URL="$site_url" "$jail/bin/node" -e '
  const { MAGELLAN_SITE_URL: site, MAGELLAN_SESSION: token, MAGELLAN_STATE_FILE: file } = process.env;
  const cookie = { name: "gram_session", value: token, domain: new URL(site).hostname, path: "/",
    expires: Math.floor(Date.now() / 1000) + 86400, httpOnly: true, secure: true, sameSite: "Lax" };
  require("node:fs").writeFileSync(file, JSON.stringify({ cookies: [cookie], origins: [] }), { mode: 0o600 });
'
chown "$run_user:$run_user" "$secret/state.json"
unset MAGELLAN_SESSION

env_file="$state/env-magellan"
{
    printf '%s=%q\n' \
        PATH "$work/bin:$jail/bin:/usr/bin:/bin" \
        JAIL_WORK "$work" \
        MAGELLAN_STATE "$secret/state.json" \
        MAGELLAN_HOME_URL "$site_url/$org" \
        MAGELLAN_TOOLS "$tools" \
        MAGELLAN_ALLOWED "$allowed" \
        MAGELLAN_BUDGET "$budget" \
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

# Inside the jail: log the browser in, delete the session file, and only then
# start the agent. Output stays in the jail user's home, out of the CI log.
# shellcheck disable=SC2016 # expanded inside the jail
inner='set -u
pw() { playwright-cli -s=magellan "$@" >/dev/null; }
cd "$JAIL_WORK/out/.pw"
pw open --config="$JAIL_WORK/cli.config.json"
pw state-load "$MAGELLAN_STATE"
rm -f "$MAGELLAN_STATE"
pw goto "$MAGELLAN_HOME_URL" || true
cd "$JAIL_WORK"
timeout "$((MAGELLAN_BUDGET + 15))m" claude -p --output-format json \
    --tools "$MAGELLAN_TOOLS" --allowedTools "$MAGELLAN_ALLOWED" --max-turns 200 \
    <prompt.md >"$HOME/claude.json" 2>"$HOME/claude.err"
echo "agent exited with $?"
pw close || true'
status=0
"$jail/jail.sh" run magellan "$env_file" "" bash -c "$inner" || status=$?
rm -rf "$secret"
echo "magellan: jail exited with ${status}"
# Only the shape of the result: anything more could describe production.
home="$state/home-magellan"
if [ -f "$home/claude.json" ] && [ ! -L "$home/claude.json" ]; then
    head -c 4000000 "$home/claude.json" |
        jq -c '{type, subtype, is_error, num_turns, duration_ms}' 2>/dev/null || echo "no result JSON"
fi

# The jail has exited, so no agent process can swap a file under us.
dest="$ws/out"
install -d -o "$owner" "$dest"
copy() {
    local src="$1" max="$2" real
    real="$(readlink -f "$src")" || return 0
    case "$real" in "$work/out/"*) ;; *) return 0 ;; esac
    if [ -f "$real" ] && [ ! -L "$src" ] && [ "$(stat -c %s "$real")" -le "$max" ]; then
        install -m 0644 -o "$owner" "$real" "$dest/$(basename "$real")"
    fi
}
copy "$work/out/findings.jsonl" 1048576
copy "$work/out/notes.md" 262144
for png in "$work"/out/*.png; do
    # Type first: reading a FIFO or device named *.png would hang or worse.
    if [ ! -f "$png" ] || [ -L "$png" ]; then continue; fi
    [[ "$(basename "$png")" =~ ^[A-Za-z0-9._-]{1,80}\.png$ ]] || continue
    [ "$(head -c 8 "$png" | od -An -tx1 | tr -d ' \n')" = 89504e470d0a1a0a ] || continue
    copy "$png" 10485760
done
