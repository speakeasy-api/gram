#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE description="Create a git worktree"
#MISE alias="gwn"
#USAGE flag "--dir <dir>" default=".." help="The directory to create the worktree in"
#USAGE flag "--branch <branch>" default="origin/main" help="The branch to check out in the worktree"
#USAGE arg "<name>" help="The name of the worktree to create"

set -e

if ! command -v git &> /dev/null; then
    echo "git command not found. Please install git to use this script."
    exit 1
fi

suffix=$(head /dev/urandom | tr -dc 'a-zA-Z0-9' | head -c 4)
new_branch="${usage_name:?}-${suffix}"
dest="${usage_dir:?}/_gram_${usage_name:?}"
git fetch
git worktree add "${dest}" "${usage_branch:?}"

cd "${dest}"
mise trust
git checkout -b "${new_branch}"

mise run git:workinit

# resolve absolute path to dest
dest=$(realpath "${dest}")
echo "To open this worktree, run:"
echo "cd ${dest}"