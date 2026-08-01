#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE description="Fuzzy-pick a git worktree and print its path (a quick worktree switcher)"
#MISE alias="gsw"
#MISE quiet=true

#USAGE flag "--open" help="Open the selected worktree's dashboard URL instead of printing its path"
#USAGE flag "--all" help="Include the current worktree in the list"

# A small TUI for hopping between git worktrees. It renders one fuzzy-filterable
# row per worktree -- current marker, branch, stack up/down, git dirtiness, and
# the dashboard URL -- and, on selection, prints the worktree's absolute path.
#
# A child process cannot change its parent shell's directory, so wire up a shell
# function once to make selection actually `cd`:
#
#   gsw() { local d; d="$(mise run git:workswitch "$@")" && [ -d "$d" ] && cd "$d"; }
#
# Then `gsw` drops you into the chosen worktree. Run without the wrapper and it
# prints a ready-to-paste `cd` line instead (stdout is a TTY, so nothing to
# capture). `gsw --open` opens the dashboard for the picked worktree.

set -euo pipefail

if ! command -v git &> /dev/null; then
  echo "git command not found. Please install git to use this script." >&2
  exit 1
fi
if ! command -v gum &> /dev/null; then
  echo "gum not found. It ships with the mise toolchain -- run 'mise install'." >&2
  exit 1
fi

open_url=${usage_open:-false}
include_current=${usage_all:-false}

current_toplevel=$(git rev-parse --show-toplevel 2>/dev/null || true)

# --- Optional liveness/URL source: worktrunk (`wt`) knows the assigned port and
# whether the stack is up. It's an opt-in tool, so everything below degrades to
# reading GRAM_SITE_PORT from each worktree's mise.local.toml when it's absent.
declare -A wt_active wt_url
if command -v wt &> /dev/null; then
  while IFS=$'\t' read -r branch active url; do
    [ -n "$branch" ] || continue
    wt_active["$branch"]=$active
    wt_url["$branch"]=$url
  done < <(wt --config-set 'list.json-schema=1' list --format json 2>/dev/null \
    | jq -r '.[] | [.branch, (.url_active | tostring), (.url // "")] | @tsv' 2>/dev/null || true)
fi

# Probe a localhost port for a listener (short timeout so a dozen worktrees stay
# snappy). Best-effort: any failure just yields "unknown".
port_is_up() {
  local port=$1
  [ -n "$port" ] || return 1
  timeout 0.25 bash -c "exec 3<>/dev/tcp/127.0.0.1/${port}" 2>/dev/null
}

site_port_of() {
  local dir=$1 port
  port=$(sed -n 's/^[[:space:]]*GRAM_SITE_PORT[[:space:]]*=[[:space:]]*"\{0,1\}\([0-9]\+\).*/\1/p' \
    "${dir}/mise.local.toml" 2>/dev/null | head -1)
  echo "${port:-5173}"
}

# Compact git-state summary for a worktree: dirty file count + ahead/behind vs
# upstream, or "clean".
git_state_of() {
  local dir=$1 dirty ahead behind out=""
  dirty=$(git -C "$dir" status --porcelain 2>/dev/null | grep -c . || true)
  if read -r behind ahead < <(git -C "$dir" rev-list --left-right --count '@{upstream}...HEAD' 2>/dev/null); then
    :
  else
    ahead=0; behind=0
  fi
  [ "${dirty:-0}" -gt 0 ] && out+="✱${dirty} "
  [ "${ahead:-0}" -gt 0 ] && out+="↑${ahead} "
  [ "${behind:-0}" -gt 0 ] && out+="↓${behind} "
  echo "${out:-clean}"
}

# --- Gather worktrees --------------------------------------------------------
paths=(); branches=(); markers=(); states=(); gits=(); urls=()
cur_path=""; cur_branch=""

while IFS= read -r line; do
  case "$line" in
    worktree\ *) cur_path=${line#worktree } ;;
    branch\ *)   cur_branch=${line#branch refs/heads/} ;;
    detached)    cur_branch="(detached)" ;;
    "")
      [ -n "$cur_path" ] || continue
      if [ "$include_current" != "true" ] && [ "$cur_path" = "$current_toplevel" ]; then
        cur_path=""; cur_branch=""; continue
      fi

      local_branch=${cur_branch:-"(unknown)"}
      if [ -n "${wt_active[$local_branch]+x}" ]; then
        [ "${wt_active[$local_branch]}" = "true" ] && state="● up" || state="○ down"
        url=${wt_url[$local_branch]}
      else
        port=$(site_port_of "$cur_path")
        url="https://localhost:${port}"
        if port_is_up "$port"; then state="● up"; else state="○ down"; fi
      fi

      [ "$cur_path" = "$current_toplevel" ] && marker="@" || marker="•"

      paths+=("$cur_path")
      branches+=("$local_branch")
      markers+=("$marker")
      states+=("$state")
      gits+=("$(git_state_of "$cur_path")")
      urls+=("$url")

      cur_path=""; cur_branch=""
      ;;
  esac
done < <(git worktree list --porcelain; echo)

if [ "${#paths[@]}" -eq 0 ]; then
  echo "No other worktrees found. Create one with 'mise run git:worknew <name>' (alias gwn)." >&2
  echo "  (pass --all to include the current worktree)" >&2
  exit 0
fi

# --- Render rows -------------------------------------------------------------
# Plain text only: gum filter shows raw text, so column alignment -- not ANSI --
# does the visual work, and the branch/url are fuzzy-matchable.
bw=6; sw=4; gw=5
for i in "${!paths[@]}"; do
  (( ${#branches[i]} > bw )) && bw=${#branches[i]}
  (( ${#states[i]}   > sw )) && sw=${#states[i]}
  (( ${#gits[i]}     > gw )) && gw=${#gits[i]}
done

displays=()
for i in "${!paths[@]}"; do
  displays+=("$(printf '%s %-*s  %-*s  %-*s  %s' \
    "${markers[i]}" "$bw" "${branches[i]}" "$sw" "${states[i]}" "$gw" "${gits[i]}" "${urls[i]}")")
done

gum style --border rounded --border-foreground 212 --padding "0 1" --margin "0 0 0 1" \
  "Worktree switcher · $([ "$open_url" = "true" ] && echo 'open dashboard' || echo 'switch') · ⏎ select · esc cancel" >&2

selection=$(printf '%s\n' "${displays[@]}" | gum filter \
  --placeholder "Type to filter worktrees…" \
  --prompt "❯ " \
  --indicator "→" \
  --height 15 \
  --select-if-one) || { echo "No worktree selected." >&2; exit 130; }

# Map the chosen row back to its path (rows are unique; trim trailing pad first).
trim() { local s=$1; s=${s%"${s##*[![:space:]]}"}; printf '%s' "$s"; }
sel_trim=$(trim "$selection")
chosen=""
for i in "${!displays[@]}"; do
  if [ "$(trim "${displays[i]}")" = "$sel_trim" ]; then chosen=${paths[i]}; chosen_url=${urls[i]}; break; fi
done

if [ -z "$chosen" ]; then
  echo "Could not resolve the selection to a worktree path." >&2
  exit 1
fi

# --- Act ---------------------------------------------------------------------
if [ "$open_url" = "true" ]; then
  echo "Opening ${chosen_url}" >&2
  if command -v open &> /dev/null; then open "$chosen_url"
  elif command -v xdg-open &> /dev/null; then xdg-open "$chosen_url"
  else echo "$chosen_url"; fi
  exit 0
fi

# Bare path on stdout for `cd "$(...)"`; a friendly hint when run interactively
# (a TTY on stdout means there's no command substitution to capture it).
if [ -t 1 ]; then
  echo "Selected worktree:" >&2
  echo "  cd ${chosen}" >&2
  echo "  (tip: define a 'gsw' shell function -- see the top of git:workswitch -- to switch in one step)" >&2
fi
echo "$chosen"
