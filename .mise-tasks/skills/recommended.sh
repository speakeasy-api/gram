#!/usr/bin/env bash

#MISE description="Install/update the recommended external agent skills from .agents/recommended-skills.json (SHA-pinned, gitignored)"
#MISE dir="{{ config_root }}"

#USAGE flag "-y --yes" help="Install without prompting (does not persist the USE_RECOMMENDED_SKILLS choice)"

set -euo pipefail

MANIFEST=".agents/recommended-skills.json"
SKILLS_DIR=".agents/skills"

if [ ! -f "$MANIFEST" ]; then
  echo "No $MANIFEST found"
  exit 1
fi

names=$(jq -r '.skills | keys[]' "$MANIFEST")
if [ -z "$names" ]; then
  echo "No recommended skills declared in $MANIFEST"
  exit 0
fi

if [ "${usage_yes:-false}" != "true" ]; then
  echo "Recommended agent skills (SHA-pinned, kept out of git; see $MANIFEST):"
  for name in $names; do
    echo "  - $name ($(jq -r ".skills[\"$name\"].repo" "$MANIFEST"))"
  done
  if command -v gum &> /dev/null; then
    gum confirm "Install them?" || exit 0
  else
    echo -n "Install them? [y/N] "
    read -r answer
    [ "$(echo "$answer" | tr '[:upper:]' '[:lower:]')" = "y" ] || exit 0
  fi
  # A manual, consented run also persists the choice so skills:sync keeps
  # the set up to date from now on.
  mise set --file mise.local.toml USE_RECOMMENDED_SKILLS=true
fi

# Keep installed skills and their harness symlinks out of git status without
# touching the committed .gitignore. info/exclude lives in the common git dir,
# so it covers every worktree of this clone.
exclude_file="$(git rev-parse --git-common-dir)/info/exclude"
mkdir -p "$(dirname "$exclude_file")"
touch "$exclude_file"
add_exclude() {
  grep -qxF "$1" "$exclude_file" || echo "$1" >> "$exclude_file"
}

for name in $names; do
  repo=$(jq -r ".skills[\"$name\"].repo" "$MANIFEST")
  ref=$(jq -r ".skills[\"$name\"].ref" "$MANIFEST")
  path=$(jq -r ".skills[\"$name\"].path" "$MANIFEST")
  dest="$SKILLS_DIR/$name"
  marker="$dest/.recommended-ref"

  for p in ".agents/skills/$name/" ".claude/skills/$name" ".codex/skills/$name" ".opencode/skills/$name" ".cursor/skills/$name"; do
    add_exclude "$p"
  done

  if [ -f "$marker" ] && [ "$(cat "$marker")" = "$ref" ]; then
    echo "✅ $name already at ${ref:0:12}"
    continue
  fi

  echo "⏳ Fetching $name from $repo@${ref:0:12}"
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  git -C "$tmp" init -q
  git -C "$tmp" remote add origin "$repo"
  # GitHub permits shallow fetches of arbitrary commit SHAs.
  git -C "$tmp" fetch -q --depth 1 origin "$ref"
  git -C "$tmp" checkout -q FETCH_HEAD -- "$path"

  rm -rf "$dest"
  mkdir -p "$dest"
  rsync -a "$tmp/$path/" "$dest/"
  echo "$ref" > "$marker"
  rm -rf "$tmp"
  trap - EXIT
  echo "✅ Installed $name -> $dest"
done
