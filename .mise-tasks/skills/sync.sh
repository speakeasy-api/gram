#!/usr/bin/env bash

#MISE description="Sync skills from .agents/skills/ into tool-specific directories (.claude, .codex, .opencode, .cursor)"
#MISE dir="{{ config_root }}"

set -euo pipefail

# Opted-in via ./zero (persisted in mise.local.toml): install/update the
# SHA-pinned external skill set before linking, so sync keeps it current.
if [ "${USE_RECOMMENDED_SKILLS:-false}" = "true" ] && [ -f ".agents/recommended-skills.json" ]; then
  mise run skills:recommended --yes
fi

SOURCE_DIR=".agents/skills"
TARGETS=(
  ".claude/skills"
  ".codex/skills"
  ".opencode/skills"
  ".cursor/skills"
)

if [ ! -d "$SOURCE_DIR" ]; then
  echo "No $SOURCE_DIR directory found"
  exit 1
fi

for target in "${TARGETS[@]}"; do
  mkdir -p "$target"

  # Remove stale symlinks pointing into .agents/skills that no longer exist
  for link in "$target"/*; do
    [ -L "$link" ] || continue
    dest=$(readlink "$link")
    if [[ "$dest" == *"$SOURCE_DIR"* ]] && [ ! -e "$link" ]; then
      echo "removing stale symlink: $link -> $dest"
      rm "$link"
    fi
  done

  # Create symlinks for each skill
  for skill in "$SOURCE_DIR"/*/; do
    skill_name=$(basename "$skill")
    link_path="$target/$skill_name"
    rel_target="../../$SOURCE_DIR/$skill_name"

    if [ -L "$link_path" ]; then
      existing=$(readlink "$link_path")
      if [ "$existing" = "$rel_target" ]; then
        continue
      fi
      rm "$link_path"
    elif [ -e "$link_path" ]; then
      # Real directory (harness-specific skill, e.g. .claude/skills/datadog-insights)
      # shadows the shared name; linking into it would nest a stray symlink inside.
      continue
    fi

    ln -s "$rel_target" "$link_path"
    echo "linked: $link_path -> $rel_target"
  done
done

echo "skills sync complete"
