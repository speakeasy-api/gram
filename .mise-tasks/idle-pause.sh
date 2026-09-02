#!/usr/bin/env bash

#MISE description="Pause this worktree's stack if nothing has used it for a while"
#MISE dir="{{ config_root }}"

#USAGE flag "--minutes <minutes>" default="60" help="Idle time before pausing"
#USAGE flag "--all" help="Check every worktree instead of this one"
#USAGE flag "--dry-run" help="Report what would be paused without pausing"
#USAGE flag "--install" help="Schedule this task to run every 5 minutes (launchd/systemd)"
#USAGE flag "--uninstall" help="Remove the schedule installed by --install"

set -e

# The complement to `mise run wake`: waking is cheap and now happens on its own
# (opening the dashboard, or any task that depends on `ensure-stack`), so
# stacks accumulate as you move between worktrees. This pauses the ones nobody
# is using.
#
# Activity signal: established TCP connections to the site and server ports. An
# open dashboard tab holds a vite HMR websocket, an agent driving the API holds
# a connection for the duration of its request, and `pitchfork` itself connects
# to neither -- so "no connections" really is "nobody is using this". A single
# quiet sample is not enough (nothing is connected between two requests), so
# the last-seen time is kept in the worktree's git dir and only a stretch of
# quiet longer than --minutes pauses the stack.
#
# `--install` schedules it every 5 minutes (a launchd agent on macOS, a systemd
# user timer on Linux). `./zero` offers this once, and only to developers who
# actually use worktrees.

minutes="${usage_minutes:-60}"
check_all="${usage_all:-false}"
dry_run="${usage_dry_run:-false}"

# The schedule runs `--all`, which walks `git worktree list` -- so it must live
# in the main worktree, the one checkout that outlives every branch.
main_worktree="$(cd "$(git rev-parse --git-common-dir)/.." && pwd)"
label="ai.getgram.gram-idle-pause"
plist="$HOME/Library/LaunchAgents/${label}.plist"
unit_dir="$HOME/.config/systemd/user"

install_schedule() {
    if [ "$(uname)" = "Darwin" ]; then
        mkdir -p "$(dirname "$plist")"
        cat > "$plist" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>${label}</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/zsh</string>
    <string>-lc</string>
    <!-- The command runs inside XML, so its and-operator is escaped. -->
    <string>cd ${main_worktree} &amp;&amp; mise run idle-pause --all --minutes ${minutes}</string>
  </array>
  <key>StartInterval</key><integer>300</integer>
  <key>RunAtLoad</key><false/>
  <key>StandardOutPath</key><string>${HOME}/Library/Logs/gram-idle-pause.log</string>
  <key>StandardErrorPath</key><string>${HOME}/Library/Logs/gram-idle-pause.log</string>
</dict>
</plist>
PLIST
        # bootout first so a re-install picks up an edited plist; a label that
        # was never loaded makes bootout fail, which is not an error here.
        launchctl bootout "gui/$(id -u)/${label}" 2> /dev/null || true
        launchctl bootstrap "gui/$(id -u)" "$plist"
        echo "Installed: launchd runs \`idle-pause --all --minutes ${minutes}\` every 5 minutes."
        echo "Log: ~/Library/Logs/gram-idle-pause.log — remove with \`mise run idle-pause --uninstall\`."
        return
    fi

    mkdir -p "$unit_dir"
    cat > "${unit_dir}/gram-idle-pause.service" << UNIT
[Unit]
Description=Pause idle Gram worktree stacks

[Service]
Type=oneshot
WorkingDirectory=${main_worktree}
ExecStart=/bin/bash -lc 'mise run idle-pause --all --minutes ${minutes}'
UNIT
    cat > "${unit_dir}/gram-idle-pause.timer" << UNIT
[Unit]
Description=Pause idle Gram worktree stacks every 5 minutes

[Timer]
OnBootSec=5min
OnUnitActiveSec=5min

[Install]
WantedBy=timers.target
UNIT
    systemctl --user daemon-reload
    systemctl --user enable --now gram-idle-pause.timer
    echo "Installed: systemd timer runs \`idle-pause --all --minutes ${minutes}\` every 5 minutes."
    echo "Logs: \`journalctl --user -u gram-idle-pause\` — remove with \`mise run idle-pause --uninstall\`."
}

uninstall_schedule() {
    if [ "$(uname)" = "Darwin" ]; then
        launchctl bootout "gui/$(id -u)/${label}" 2> /dev/null || true
        rm -f "$plist"
        echo "Removed the launchd agent."
        return
    fi
    systemctl --user disable --now gram-idle-pause.timer 2> /dev/null || true
    rm -f "${unit_dir}/gram-idle-pause.service" "${unit_dir}/gram-idle-pause.timer"
    systemctl --user daemon-reload 2> /dev/null || true
    echo "Removed the systemd timer."
}

if [ "${usage_install:-false}" = "true" ]; then
    install_schedule
    exit 0
fi

if [ "${usage_uninstall:-false}" = "true" ]; then
    uninstall_schedule
    exit 0
fi

if [ "$check_all" = "true" ]; then
    # Each worktree has its own ports and its own git dir, and mise resolves
    # both from the directory -- so recurse per worktree rather than trying to
    # read another worktree's environment from here.
    forward=(--minutes "$minutes")
    [ "$dry_run" = "true" ] && forward+=(--dry-run)

    while read -r wt; do
        [ -d "$wt" ] || continue
        # Worktrees on a branch that predates this task would just print
        # "no task idle-pause found" on every cron run -- skip them quietly.
        (cd "$wt" && mise tasks info idle-pause &> /dev/null \
            && mise run idle-pause "${forward[@]}") || true
    done < <(git worktree list --porcelain | sed -n 's/^worktree //p')
    exit 0
fi

gitdir="$(git rev-parse --absolute-git-dir)"
branch="$(git branch --show-current 2> /dev/null || echo detached)"

# Already paused, or a boot is in flight: nothing to do either way.
[ -f "$gitdir/gram-stack-paused" ] && exit 0
if [ -f "$gitdir/gram-stack-boot.pid" ]; then
    pid=$(cat "$gitdir/gram-stack-boot.pid")
    ps -o command= -p "$pid" 2> /dev/null | grep -q workboot && exit 0
fi

# Nothing listening on the site port means the stack is down (not paused --
# a nuked or never-booted worktree); leave it alone.
if ! lsof -nP -iTCP:"${GRAM_SITE_PORT}" -sTCP:LISTEN -t > /dev/null 2>&1; then
    exit 0
fi

conns=$(lsof -nP \
    -iTCP:"${GRAM_SITE_PORT}" -iTCP:"${GRAM_SERVER_PORT}" \
    -sTCP:ESTABLISHED -t 2> /dev/null | wc -l | tr -d ' ')

stamp="$gitdir/gram-stack-lastseen"
now=$(date +%s)

if [ "$conns" -gt 0 ]; then
    echo "$now" > "$stamp"
    exit 0
fi

# First quiet sample after a wake has no stamp to compare against. Start the
# clock now rather than pausing a stack that came up seconds ago.
if [ ! -f "$stamp" ]; then
    echo "$now" > "$stamp"
    exit 0
fi

idle=$(( (now - $(cat "$stamp")) / 60 ))
if [ "$idle" -lt "$minutes" ]; then
    exit 0
fi

if [ "$dry_run" = "true" ]; then
    echo "${branch}: idle ${idle}m — would pause"
    exit 0
fi

echo "${branch}: idle ${idle}m — pausing"
rm -f "$stamp"
mise run pause
