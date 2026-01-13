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
    mkdir -p "$dest_path/api-reference"
    # Sync API docs to api-reference subfolder
    rsync -a --delete \
      --exclude='.DS_Store' \
      --exclude='/index.mdx' \
      --exclude='_media' \
      --exclude='README.md' \
      --filter='P /index.mdx' \
      "$src_path/" "$dest_path/api-reference/"

    # Protect root-level files
    touch "$dest_path/.keep" 2>/dev/null || true

    # Use _media/README.md as quickstart.md at root level
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

    # Special case: globals.md becomes index.md for api-reference
    is_index_page=""
    if [ "$filename" = "globals" ]; then
      is_index_page="true"
      kebab_filename="index"
      title="API Reference"
    fi

    # Rename file to kebab-case (or index for globals)
    if [ "$filename" != "$kebab_filename" ]; then
      mv "$f" "$dir/$kebab_filename.md"
      f="$dir/$kebab_filename.md"
      echo "  Renamed: $filename.md -> $kebab_filename.md"
    fi

    # Strip typedoc header lines (version link, separator, breadcrumb)
    tmp=$(mktemp)
    sed -E '/^\[\*\*@gram-ai\/elements/d; /^@gram-ai\/elements/d; /^\[@gram-ai\/elements\]/d; /^\*\*\*$/d' "$f" | \
      sed '/./,$!d' > "$tmp"
    mv "$tmp" "$f"

    # Add frontmatter if not already present
    if ! head -1 "$f" | grep -q '^---$'; then
      tmp=$(mktemp)
      {
        echo "---"
        echo "title: $title"
        if [ -n "$is_index_page" ]; then
          echo "asIndexPage: true"
        fi
        echo "---"
        echo ""
        cat "$f"
      } > "$tmp"
      mv "$tmp" "$f"
      echo "  Added frontmatter to: $(basename "$f")"
    fi
  done
}

transform_to_mdx() {
  echo "==> Transforming markdown to MDX (callouts, h3 separators, h4 to italic)"

  # Process .md files - transform and convert to .mdx
  find "$dest_path" -type f -name '*.md' | while read -r f; do
    has_callouts=""
    has_h3=""
    has_h4=""

    # Check for callouts and h3/h4 headings
    if grep -qE '^> \*\*(Note|Warning|Tip|Important|Caution):\*\*' "$f"; then
      has_callouts="true"
    fi
    if grep -qE '^### ' "$f"; then
      has_h3="true"
    fi
    if grep -qE '^#### ' "$f"; then
      has_h4="true"
    fi

    # Skip if no transformations needed
    if [ -z "$has_callouts" ] && [ -z "$has_h3" ] && [ -z "$has_h4" ]; then
      continue
    fi

    tmp=$(mktemp)

    # Transform callouts, add hr before h3 headings, convert h4 to italic
    perl -pe '
      # Transform callouts
      s/^> \*\*Note:\*\* ?(.*)$/<Callout type="info">$1<\/Callout>/;
      s/^> \*\*Tip:\*\* ?(.*)$/<Callout type="info">$1<\/Callout>/;
      s/^> \*\*Warning:\*\* ?(.*)$/<Callout type="warning">$1<\/Callout>/;
      s/^> \*\*Important:\*\* ?(.*)$/<Callout type="warning">$1<\/Callout>/;
      s/^> \*\*Caution:\*\* ?(.*)$/<Callout type="warning">$1<\/Callout>/;
      # Add hr separator before h3 headings
      s/^(### .*)$/\n<hr className="api-docs-separator" \/>\n\n$1/;
      # Convert h4 headings to italic
      s/^#### (.*)$/*$1*/;
    ' "$f" > "$tmp"

    # Add import statement after frontmatter if callouts were used
    if [ -n "$has_callouts" ]; then
      perl -i -pe '
        if (/^---$/ && $seen_first) {
          $_ .= "\nimport { Callout } from \"\@/mdx/components\";\n";
          $done = 1;
        }
        $seen_first = 1 if /^---$/ && !$seen_first;
      ' "$tmp"
    fi

    mv "$tmp" "$f"

    # Rename .md to .mdx
    new_file="${f%.md}.mdx"
    mv "$f" "$new_file"
    echo "  Converted to MDX: $(basename "$new_file")"
  done
}

generate_indexes() {
  echo "==> Generating index.md for subdirectories"

  api_ref_path="$dest_path/api-reference"

  declare -A titles=(
    ["interfaces"]="Interfaces"
    ["functions"]="Functions"
    ["type-aliases"]="Type Aliases"
    ["variables"]="Variables"
  )

  for dir in "${!titles[@]}"; do
    dir_path="$api_ref_path/$dir"
    if [ -d "$dir_path" ]; then
      index_file="$dir_path/index.md"
      title="${titles[$dir]}"

      echo "---" > "$index_file"
      echo "title: $title" >> "$index_file"
      echo "asIndexPage: true" >> "$index_file"
      echo "---" >> "$index_file"
      echo "" >> "$index_file"

      # List all .md and .mdx files except index.md, sorted alphabetically
      for f in $(find "$dir_path" -maxdepth 1 \( -name '*.md' -o -name '*.mdx' \) ! -name 'index.md' -type f | sort); do
        # Strip either .md or .mdx extension
        filename=$(basename "$f" | sed 's/\.mdx\?$//')
        # Extract original title from frontmatter
        file_title=$(sed -n 's/^title: //p' "$f" | head -1)
        if [ -z "$file_title" ]; then
          file_title="$filename"
        fi
        echo "- [$file_title](${docs_prefix}/api-reference/${dir}/${filename})" >> "$index_file"
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

        # Get relative directory of current file
        rel_dir=$(dirname "${f#$dest_path/}")
        if [ "$rel_dir" = "." ]; then
          rel_dir=""
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
          my $current_dir = "'"$rel_dir"'";

          s{\]\(([^)#]+?)\.md(#[^)]+)?\)}{
            my $path = $1;
            my $anchor = $2 // "";
            # Skip external URLs
            if ($path =~ /^(https?:|mailto:)/) {
              "](${path}.md${anchor})";
            } else {
              # Handle relative paths (../)
              my $base_dir = $current_dir;
              while ($path =~ s{^\.\./}{}) {
                $base_dir =~ s{/?[^/]+$}{};
              }
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
              } elsif ($path eq "quickstart") {
                "](${prefix}/quickstart${anchor})";
              } elsif ($path eq "globals" || $path =~ m{^api-reference/globals$}i) {
                "](${prefix}/api-reference${anchor})";
              } else {
                # If path has no directory and we have a current dir, prepend it
                if ($path !~ m{/} && $base_dir ne "") {
                  $path = "$base_dir/$path";
                }
                # Strip api-reference/ if already present (for consistency)
                $path =~ s{^api-reference/}{};
                # Convert each path segment to kebab-case
                my @parts = split("/", $path);
                @parts = map { to_kebab($_) } @parts;
                my $new_path = join("/", @parts);
                "](${prefix}/api-reference/${new_path}${anchor})";
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
transform_to_mdx
generate_indexes
transform_links

echo "==> Done!"
