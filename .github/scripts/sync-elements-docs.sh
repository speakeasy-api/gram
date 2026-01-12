#!/usr/bin/env bash
set -euo pipefail

# Sync Elements docs to marketing site
# Usage: ./sync-elements-docs.sh <src_path> <dest_path> <docs_prefix>
# Example: ./sync-elements-docs.sh "elements/docs" "marketing-repo/src/content/docs/gram-elements" "/docs/gram-elements"

src_path="${1:?Usage: $0 <src_path> <dest_path> <docs_prefix>}"
dest_path="${2:?Usage: $0 <src_path> <dest_path> <docs_prefix>}"
docs_prefix="${3:?Usage: $0 <src_path> <dest_path> <docs_prefix>}"

# Convert to kebab-case:
# - SCREAMING_SNAKE_CASE -> kebab-case (COLOR_SCHEMES -> color-schemes)
# - PascalCase/camelCase -> kebab-case (ModalTriggerPosition -> modal-trigger-position)
to_kebab() {
  echo "$1" | sed -E 's/_/-/g; s/([a-z])([A-Z])/\1-\2/g; s/([A-Z]+)([A-Z][a-z])/\1-\2/g' | tr '[:upper:]' '[:lower:]'
}

sync_docs() {
  echo "==> Syncing docs from $src_path to $dest_path"
  if [ -d "$src_path" ]; then
    mkdir -p "$dest_path"
    rsync -a --delete \
      --exclude='.DS_Store' \
      --exclude='/index.mdx' \
      --exclude='_media' \
      --exclude='README.md' \
      --filter='P /index.mdx' \
      --filter='P /plugins.mdx' \
      "$src_path/" "$dest_path/"

    # Use _media/README.md as quickstart.md
    if [ -f "$src_path/_media/README.md" ]; then
      cp "$src_path/_media/README.md" "$dest_path/quickstart.md"
      echo "  Copied: _media/README.md -> quickstart.md"
    fi
  else
    echo "Warning: $src_path does not exist; nothing to sync."
    exit 1
  fi
}

normalize_files() {
  echo "==> Normalizing filenames and adding frontmatter"
  find "$dest_path" -type f -name '*.md' | while read -r f; do
    dir=$(dirname "$f")
    filename=$(basename "$f" .md)
    kebab_filename=$(to_kebab "$filename")
    title="$filename"

    # Special case: quickstart.md gets "Quickstart" title
    if [ "$filename" = "quickstart" ]; then
      title="Quickstart"
    fi

    # Rename file to kebab-case if needed
    if [ "$filename" != "$kebab_filename" ]; then
      mv "$f" "$dir/$kebab_filename.md"
      f="$dir/$kebab_filename.md"
      echo "  Renamed: $filename.md -> $kebab_filename.md"
    fi

    # Add frontmatter if not already present
    if ! head -1 "$f" | grep -q '^---$'; then
      tmp=$(mktemp)
      {
        echo "---"
        echo "title: $title"
        echo "---"
        echo ""
        cat "$f"
      } > "$tmp"
      mv "$tmp" "$f"
      echo "  Added frontmatter to: $(basename "$f")"
    fi
  done
}

generate_indexes() {
  echo "==> Generating index.md for subdirectories"

  declare -A titles=(
    ["interfaces"]="Interfaces"
    ["functions"]="Functions"
    ["type-aliases"]="Type Aliases"
    ["variables"]="Variables"
  )

  for dir in "${!titles[@]}"; do
    dir_path="$dest_path/$dir"
    if [ -d "$dir_path" ]; then
      index_file="$dir_path/index.md"
      title="${titles[$dir]}"

      echo "---" > "$index_file"
      echo "title: $title" >> "$index_file"
      echo "asIndexPage: true" >> "$index_file"
      echo "---" >> "$index_file"
      echo "" >> "$index_file"

      # List all .md files except index.md, sorted alphabetically
      for f in $(find "$dir_path" -maxdepth 1 -name '*.md' ! -name 'index.md' -type f | sort); do
        filename=$(basename "$f" .md)
        # Extract original title from frontmatter
        file_title=$(sed -n 's/^title: //p' "$f" | head -1)
        if [ -z "$file_title" ]; then
          file_title="$filename"
        fi
        echo "- [$file_title](${docs_prefix}/${dir}/${filename})" >> "$index_file"
      done

      echo "  Generated: $index_file"
    fi
  done
}

transform_links() {
  echo "==> Transforming internal links"

  if [ -d "$dest_path" ]; then
    mapfile -t files < <(find "$dest_path" -type f \( -name '*.md' -o -name '*.mdx' -o -name '*.markdown' \))
    if [ "${#files[@]}" -gt 0 ]; then
      for f in "${files[@]}"; do
        # Skip the root-level index.mdx
        if [[ "$f" == "$dest_path/index.mdx" ]]; then
          continue
        fi

        # Transform internal markdown links:
        # [Text](path/FileName.md) -> [Text](/docs/gram-elements/path/file-name)
        perl -i -pe '
          sub to_kebab {
            my $s = shift;
            $s =~ s/_/-/g;
            $s =~ s/([a-z])([A-Z])/$1-$2/g;
            $s =~ s/([A-Z]+)([A-Z][a-z])/$1-$2/g;
            return lc($s);
          }

          my $prefix = "'"$docs_prefix"'";

          s{\]\(([^)#]+?)\.md(#[^)]+)?\)}{
            my $path = $1;
            my $anchor = $2 // "";
            # Skip external URLs
            if ($path =~ /^(https?:|mailto:)/) {
              "](${path}.md${anchor})";
            } else {
              # Strip relative path prefixes
              $path =~ s{^\./}{};
              $path =~ s{^docs/}{};
              # Strip _media/ prefix since we exclude that folder
              $path =~ s{^_media/}{};
              # README.md -> quickstart (since we rename it)
              $path =~ s{^README$}{quickstart}i;
              # src/plugins/README -> plugins (link to plugin guide)
              $path =~ s{^src/plugins/README$}{plugins}i;
              # interfaces/Plugin -> plugins (link to plugin guide)
              if ($path =~ m{^interfaces/Plugin$}i || $path eq "plugins") {
                "](${prefix}/plugins${anchor})";
              } else {
                # Convert each path segment to kebab-case
                my @parts = split("/", $path);
                @parts = map { to_kebab($_) } @parts;
                my $new_path = join("/", @parts);
                "](${prefix}/${new_path}${anchor})";
              }
            }
          }ge;
        ' "$f"
      done
      echo "  Transformed links in ${#files[@]} files"
    fi
  fi
}

# Run all steps
sync_docs
normalize_files
generate_indexes
transform_links

echo "==> Done!"
