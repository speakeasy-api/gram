#!/usr/bin/env bash
# <swiftbar.title>Speakeasy worktrees</swiftbar.title>
# <swiftbar.desc>Each worktree's branch and dashboard URL. Click to open, Option-click to copy.</swiftbar.desc>
# <swiftbar.hideAbout>true</swiftbar.hideAbout>
# <swiftbar.hideRunInTerminal>true</swiftbar.hideRunInTerminal>
# <swiftbar.hideLastUpdated>true</swiftbar.hideLastUpdated>
# <swiftbar.hideDisablePlugin>true</swiftbar.hideDisablePlugin>
# <swiftbar.hideSwiftBar>true</swiftbar.hideSwiftBar>

# SwiftBar plugin: lists every worktree of this repository with its dashboard
# URL and whether its stack is running, paused or stopped. Installed by
# `mise run goodies:menubar`. The ".10s" in the file name is SwiftBar's refresh
# interval.

export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

# SwiftBar runs the symlink in its plugin folder; follow it back to the repo,
# then to the main checkout so any worktree's copy finds all of them.
self=$(readlink "$0" || echo "$0")
common_dir=$(git -C "$(dirname "$self")" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)
if [[ -z $common_dir ]]; then
  echo "| sfimage=exclamationmark.triangle"
  echo "---"
  echo "Repository not found at $(dirname "$self")"
  echo "Re-run mise run goodies:menubar"
  exit 0
fi
REPO=$(dirname "$common_dir")

# One lsof call: "port pid" lines for every listening TCP socket.
listeners=$(lsof -nP -iTCP -sTCP:LISTEN -Fpn 2>/dev/null |
  awk '/^p/{pid=substr($0,2)} /^n/{n=split($0,a,":"); print a[n], pid}')

# Read a key from a worktree's mise.local.toml ("KEY = \"value\"").
local_var() {
  sed -nE "s/^$1[[:space:]]*=[[:space:]]*\"([^\"]*)\".*/\1/p" "$2/mise.local.toml" 2>/dev/null | head -1
}

running=() paused=() stopped=()
while IFS=$'\t' read -r path branch; do
  port=$(local_var GRAM_SITE_PORT "$path")
  # The main checkout uses the mise.toml default; worktrees without a port never booted a stack.
  [[ -z $port && $path == "$REPO" ]] && port=5173
  [[ -z $port ]] && continue
  host=$(local_var GRAM_HOST "$path")
  host=${host:-localhost}
  scheme=$(local_var GRAM_HTTP_SCHEME "$path")
  scheme=${scheme:-https}
  # The URL ends up in SwiftBar attributes and a shell command; skip anything
  # that is not a plain scheme, hostname and port.
  [[ $port =~ ^[0-9]+$ && $host =~ ^[A-Za-z0-9.-]+$ && $scheme =~ ^https?$ ]] || continue
  url="$scheme://$host:$port"
  # `|` separates SwiftBar's label from its attributes and `"` ends a quoted attribute.
  branch=${branch//|/¦}
  branch=${branch//\"/\'}
  pid=$(awk -v p="$port" '$1==p{print $2; exit}' <<<"$listeners")
  line="$branch  :$port"
  if [[ -z $pid ]]; then
    stopped+=("$line|$url")
  elif ps -o args= -p "$pid" | grep -q '\.mise-tasks/park\.mts'; then
    # `mise run pause` parks .mise-tasks/park.mts on the site port.
    paused+=("$line|$url")
  else
    running+=("$line|$url")
  fi
done < <(git -C "$REPO" worktree list --porcelain | awk '
  /^worktree /{p=substr($0,10)}
  /^branch /{b=substr($0,8); sub("refs/heads/","",b)}
  /^detached/{b="(detached)"}
  /^$/{if(p!="") print p "\t" b; p=""; b=""}
  END{if(p!="") print p "\t" b}')

GRAY=$'\e[90m' RESET=$'\e[0m'

# $1 = "branch  :port|url", $2 = ANSI color code for the status dot, $3 = submenu prefix.
# SwiftBar draws SF Symbols in menu items as black template images and ignores
# sfcolor there, so the status dot is a colored text glyph instead.
item() {
  local branch=${1%%  :*} rest=${1#*  :} url=${1##*|}
  local port=${rest%%|*}
  # Drop the owner prefix and the random worktree suffix: "someone/foo-1a2b3c4d" -> "foo".
  local name=${branch#*/}
  name=$(sed -E 's/-[0-9a-f]{8}$//' <<<"$name")
  local dot=$'\e['"$2"$'m●'"$RESET"
  local common="ansi=true tooltip=\"$branch — $url\""
  echo "$3$dot  $name  ${GRAY}:$port${RESET} | href=$url $common"
  echo "$3$dot  $name  ${GRAY}copy $url${RESET} | alternate=true $common bash=/bin/sh param1=-c param2=\"printf %s $url | pbcopy\" terminal=false"
}

# Items with no action are already grayed out by macOS; extra color makes them unreadable.
header() { echo "$1 | size=12"; }

sorted() { printf '%s\n' "$@" | sort -f; }

if (( ${#running[@]} )); then
  echo "${#running[@]} | sfimage=circle.hexagongrid.fill"
else
  echo "| sfimage=circle.hexagongrid"
fi
echo "---"
header "RUNNING"
if (( ${#running[@]} )); then
  while read -r x; do item "$x" 32; done < <(sorted "${running[@]}")
else
  echo "Nothing running"
fi
if (( ${#paused[@]} )); then
  echo "---"
  header "PAUSED · opens the resume page"
  while read -r x; do item "$x" 33; done < <(sorted "${paused[@]}")
fi
if (( ${#stopped[@]} )); then
  echo "---"
  echo "Stopped  ${GRAY}${#stopped[@]}${RESET} | sfimage=moon.zzz ansi=true"
  while read -r x; do item "$x" 31 "--"; done < <(sorted "${stopped[@]}")
fi
echo "---"
echo "Refresh | sfimage=arrow.clockwise refresh=true key=CmdOrCtrl+r"
