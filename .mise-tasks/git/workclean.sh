#!/usr/bin/env bash

#MISE description="Select from a list of git worktrees to be cleaned up"
#MISE alias="gwc"

#USAGE flag "--forceclean" help="Force removal of worktrees without confirmation"

set -e

if ! command -v git &> /dev/null; then
    echo "git command not found. Please install git to use this script."
    exit 1
fi

current_worktree=$(git rev-parse --show-toplevel)
if [ -z "$current_worktree" ]; then
    echo "No git repository found in the current directory."
    exit 1
fi

# Get the list of worktrees excluding the current one
worktrees=$(git worktree list | grep -v "$current_worktree" | awk '{print $1}')
if [ -z "$worktrees" ]; then
    echo "No other worktrees found to clean up."
    exit 0
fi

# Use gum cli to choose which to clean. Allows multiple selections.
selected_worktrees=$(echo "$worktrees" | gum choose --no-limit --header "Select worktrees to clean up:")

if [ -z "$selected_worktrees" ]; then
    echo "No worktrees selected for cleanup."
    exit 0
fi

echo
echo "🚨 Selected worktrees for cleanup:"
echo "$selected_worktrees" | tr ' ' '\n' | sort -u
echo
gum confirm "Are you sure you want to clean up these worktrees?" || {
    echo "Cleanup cancelled."
    exit 0
}

flags=()
if [ "${usage_forceclean:-false}" = "true" ]; then
    flags+=("--force")
fi

# Loop through each selected worktree and remove it
while IFS= read -r worktree; do
    echo "Cleaning up worktree: $worktree"
    (cd "$worktree" && mise run nuke)
    git worktree remove "${flags[@]}" "$worktree"
done <<< "$selected_worktrees"
echo "Selected worktrees have been cleaned up successfully."