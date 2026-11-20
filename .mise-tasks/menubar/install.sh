#!/usr/bin/env bash

#MISE description="Install a macOS menu bar item that lists each worktree's dashboard URL and stack state"
#MISE dir="{{ config_root }}"
#MISE quiet=true

set -euo pipefail

if [[ $(uname) != Darwin ]]; then
  echo "The menu bar item needs macOS (it runs as a SwiftBar plugin)." >&2
  exit 1
fi

plugin=speakeasy-worktrees.10s.sh

# Link the main checkout's copy, not this worktree's, so removing the worktree
# does not break the menu bar item. Fall back to this checkout before the
# plugin has reached the main checkout.
main=$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")
target="$main/tools/menubar/$plugin"
if [[ ! -f $target ]]; then
  target="$PWD/tools/menubar/$plugin"
  echo "The main checkout has no $plugin yet; linking this worktree's copy. Re-run after you pull main."
fi

if [[ ! -d /Applications/SwiftBar.app ]]; then
  echo "Installing SwiftBar..."
  brew install --cask swiftbar
fi

# Keep a plugin folder someone already chose; set the default one otherwise,
# which also skips SwiftBar's first-run folder prompt.
dir=$(defaults read com.ameba.SwiftBar PluginDirectory 2>/dev/null || true)
if [[ -z $dir ]]; then
  dir="$HOME/Library/Application Support/SwiftBar/Plugins"
  defaults write com.ameba.SwiftBar PluginDirectory "$dir"
fi
mkdir -p "$dir"
ln -sf "$target" "$dir/$plugin"
echo "Linked $dir/$plugin -> $target"

if pgrep -xq SwiftBar; then
  open -g "swiftbar://refreshallplugins"
else
  open -a SwiftBar
fi
echo "Done. Turn on \"Launch at login\" in SwiftBar's preferences to keep it running."
