#!/usr/bin/env bash
# The agent jail: run an AI agent so that nothing it is told to do can reach a
# secret or the internet. See README.md for the threat model. Run as root.
#
#   jail.sh install <claude-version> [npm-package@version ...]
#       Install node, Claude Code and any extra packages into /opt/agent-jail,
#       owned by root and read-only to everyone else.
#   jail.sh browsers
#       Install the Chromium builds those Playwright packages need.
#   jail.sh start <port> [<port> ...]
#       Create the jail users, start the egress proxy (reads the Anthropic key
#       from ANTHROPIC_API_KEY, keeps it in a file only its own user can read)
#       and one Unix-socket bridge per local port the agent may reach. The
#       proxy's port is always bridged.
#   jail.sh run <name> <env-file> <port,port,...> <command> [args ...]
#       Run <command> as the jail user, in fresh network, PID, mount, IPC and
#       UTS namespaces, with /home, /root, /tmp and other places that hold the
#       runner's files hidden, no capabilities and no way to gain any. Its only
#       sockets out are the bridges for the listed ports. <env-file> holds the
#       KEY=value lines of its environment; nothing else is inherited.
#   jail.sh stop
#       Stop the proxy and the bridges.

set -euo pipefail

JAIL_ROOT=/opt/agent-jail
JAIL_STATE=/var/lib/agent-jail
JAIL_RUN=/run/agent-jail
JAIL_LOG=/var/log/agent-jail
AGENT_USER=agentjail
PROXY_USER=agentproxy
PROXY_PORT="${JAIL_PROXY_PORT:-3128}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

die() {
    echo "agent-jail: $*" >&2
    exit 1
}

[ "$(id -u)" = 0 ] || die "run as root"

ensure_user() {
    id -u "$1" >/dev/null 2>&1 ||
        useradd --system --no-create-home --home-dir /nonexistent \
            --shell /usr/sbin/nologin --user-group "$1"
}

cmd_install() {
    local claude_version="${1:?claude version}"
    shift
    local node_bin
    node_bin="${NODE_BIN:-$(command -v node)}"
    [ -x "$node_bin" ] || die "node not found; set NODE_BIN"
    local npm_cli
    npm_cli="$(dirname "$(readlink -f "$node_bin")")/../lib/node_modules/npm/bin/npm-cli.js"
    [ -f "$npm_cli" ] || die "npm not found next to $node_bin"

    install -d -m 0755 "$JAIL_ROOT" "$JAIL_ROOT/bin" "$JAIL_ROOT/tools" "$JAIL_ROOT/browsers"
    install -m 0755 "$(readlink -f "$node_bin")" "$JAIL_ROOT/bin/node"
    # The jail hides /home, where the checkout lives, so it runs these copies.
    cp "$here/bridge.mjs" "$here/proxy.mjs" "$here/inside.sh" "$here/jail.sh" "$JAIL_ROOT/"

    # Exact versions, integrity-checked by npm, with install scripts off. Claude
    # Code's own script (it links its native binary) is the one run, by hand:
    # newer npm refuses install scripts unless each is allowlisted.
    (
        cd "$JAIL_ROOT/tools"
        [ -f package.json ] || echo '{"private":true}' >package.json
        HOME="$JAIL_ROOT/tools" "$JAIL_ROOT/bin/node" "$npm_cli" install \
            --no-audit --no-fund --save-exact --ignore-scripts \
            "@anthropic-ai/claude-code@${claude_version}" "$@"
        cd node_modules/@anthropic-ai/claude-code
        if [ -f install.cjs ]; then
            HOME="$JAIL_ROOT/tools" "$JAIL_ROOT/bin/node" install.cjs
        fi
    )
    ln -sf "$JAIL_ROOT/tools/node_modules/.bin/claude" "$JAIL_ROOT/bin/claude"
    if [ -e "$JAIL_ROOT/tools/node_modules/.bin/playwright-cli" ]; then
        ln -sf "$JAIL_ROOT/tools/node_modules/.bin/playwright-cli" "$JAIL_ROOT/bin/playwright-cli"
    fi
    chown -R root:root "$JAIL_ROOT"
    chmod -R a+rX,go-w "$JAIL_ROOT"
    PATH="$JAIL_ROOT/bin:$PATH" "$JAIL_ROOT/bin/claude" --version >/dev/null ||
        die "Claude Code did not install"
}

# Browsers for every Playwright in the tools, installed while root still has
# the network. The jail points PLAYWRIGHT_BROWSERS_PATH here.
cmd_browsers() {
    local cli
    export PLAYWRIGHT_BROWSERS_PATH="$JAIL_ROOT/browsers"
    for cli in "$JAIL_ROOT"/tools/node_modules/playwright/cli.js \
        "$JAIL_ROOT"/tools/node_modules/@playwright/cli/node_modules/playwright/cli.js; do
        if [ -f "$cli" ]; then "$JAIL_ROOT/bin/node" "$cli" install --with-deps chromium; fi
    done
    if [ -e "$JAIL_ROOT/bin/playwright-cli" ]; then
        PATH="$JAIL_ROOT/bin:$PATH" playwright-cli install-browser chromium --with-deps
    fi
    chmod -R a+rX,go-w "$JAIL_ROOT/browsers"
}

cmd_start() {
    : "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY is required}"
    ensure_user "$AGENT_USER"
    ensure_user "$PROXY_USER"
    local agent_uid
    agent_uid="$(id -u "$AGENT_USER")"

    install -d -m 0755 "$JAIL_STATE" "$JAIL_RUN"
    install -d -m 0700 -o "$PROXY_USER" -g "$PROXY_USER" "$JAIL_RUN/proxy" "$JAIL_LOG"
    install -d -m 0700 -o "$AGENT_USER" -g "$AGENT_USER" "$JAIL_RUN/sock"

    # The key goes to a file only the proxy's user can read. printf is a
    # builtin, so the key never appears in any process's arguments.
    local key_file="$JAIL_RUN/proxy/key"
    (umask 077 && printf '%s' "$ANTHROPIC_API_KEY" >"$key_file")
    chown "$PROXY_USER:$PROXY_USER" "$key_file"

    # The proxy runs as its own user with nothing inherited but what it needs.
    setsid setpriv --reuid="$PROXY_USER" --regid="$PROXY_USER" --init-groups \
        --no-new-privs --inh-caps=-all --bounding-set=-all -- \
        env -i PATH=/usr/bin:/bin \
        JAIL_PROXY_PORT="$PROXY_PORT" \
        JAIL_KEY_FILE="$key_file" \
        JAIL_ALLOWED_HOSTS="${JAIL_ALLOWED_HOSTS:-}" \
        JAIL_PROXY_LOG="$JAIL_LOG/proxy.log" \
        "$JAIL_ROOT/bin/node" "$JAIL_ROOT/proxy.mjs" \
        >"$JAIL_LOG/proxy.out" 2>&1 </dev/null &
    echo $! >"$JAIL_RUN/proxy.pid"

    local port
    for port in "$PROXY_PORT" "$@"; do
        [[ "$port" =~ ^[0-9]+$ ]] || die "bad port: $port"
        setsid "$JAIL_ROOT/bin/node" "$JAIL_ROOT/bridge.mjs" unix-to-tcp \
            "$JAIL_RUN/sock/$port.sock" "$port" "$agent_uid" \
            >>"$JAIL_LOG/bridges.out" 2>&1 </dev/null &
        echo $! >>"$JAIL_RUN/bridges.pid"
    done

    local _
    for _ in $(seq 1 50); do
        local ready=1
        for port in "$PROXY_PORT" "$@"; do
            [ -S "$JAIL_RUN/sock/$port.sock" ] || ready=0
        done
        if [ "$ready" = 1 ] && (exec 3<>"/dev/tcp/127.0.0.1/$PROXY_PORT") 2>/dev/null; then
            echo "agent-jail: proxy and bridges are up"
            return 0
        fi
        sleep 0.2
    done
    die "proxy or bridges did not start; see $JAIL_LOG"
}

cmd_stop() {
    local f
    for f in "$JAIL_RUN/proxy.pid" "$JAIL_RUN/bridges.pid"; do
        [ -f "$f" ] || continue
        xargs -r kill <"$f" 2>/dev/null || true
        rm -f "$f"
    done
    rm -f "$JAIL_RUN/proxy/key"
}

# Runs inside the new namespaces, still as root: hide the host's files, bring
# up loopback, then drop to the jail user for good.
cmd_enter() {
    local name="$1" env_file="$2" ports="$3"
    shift 3

    mount --make-rprivate /
    # Everything that holds the runner's files, credentials or other users'
    # data. The agent works under $JAIL_STATE and runs tools from $JAIL_ROOT.
    local d
    for d in /home /root /mnt /media /srv /run/user /blacksmith-git-mirror \
        "$JAIL_LOG" "$JAIL_RUN/proxy"; do
        if [ -d "$d" ]; then mount -t tmpfs -o nosuid,nodev,noexec,mode=0755 tmpfs "$d"; fi
    done
    for d in /tmp /var/tmp /dev/shm; do
        if [ -d "$d" ]; then mount -t tmpfs -o nosuid,nodev,mode=1777 tmpfs "$d"; fi
    done
    ip link set lo up

    local uid gid
    uid="$(id -u "$AGENT_USER")"
    gid="$(id -g "$AGENT_USER")"
    exec setpriv --reuid="$uid" --regid="$gid" --clear-groups \
        --no-new-privs --inh-caps=-all --bounding-set=-all -- \
        env -i PATH="$JAIL_ROOT/bin:/usr/bin:/bin" HOME="$JAIL_STATE/home-$name" \
        SHELL=/bin/bash TMPDIR=/tmp LANG=C.UTF-8 \
        bash "$JAIL_ROOT/inside.sh" "$env_file" "$ports" "$JAIL_RUN/sock" "$@"
}

cmd_run() {
    local name="${1:?name}" env_file="${2:?env file}" ports="${3?ports}"
    shift 3
    [[ "$name" =~ ^[a-z0-9-]+$ ]] || die "bad name: $name"
    [[ "$ports" =~ ^[0-9,]*$ ]] || die "bad ports: $ports"
    # It must still be visible once /home and /tmp are hidden.
    case "$env_file" in "$JAIL_STATE"/*) ;; *) die "env file must be under $JAIL_STATE" ;; esac
    [ -f "$env_file" ] || die "no env file: $env_file"
    install -d -m 0700 -o "$AGENT_USER" -g "$AGENT_USER" "$JAIL_STATE/home-$name"
    command -v ip >/dev/null || die "ip (iproute2) is required"
    exec unshare --net --pid --mount --ipc --uts --fork --mount-proc --kill-child -- \
        bash "$JAIL_ROOT/jail.sh" enter "$name" "$env_file" "$PROXY_PORT,$ports" "$@"
}

sub="${1:-}"
shift || true
case "$sub" in
    install) cmd_install "$@" ;;
    browsers) cmd_browsers ;;
    start) cmd_start "$@" ;;
    run) cmd_run "$@" ;;
    enter) cmd_enter "$@" ;;
    stop) cmd_stop ;;
    *) die "usage: jail.sh install|start|run|stop (see the header)" ;;
esac
