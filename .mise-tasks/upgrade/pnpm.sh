#!/usr/bin/env bash

#MISE description="Upgrade pnpm version in mise.toml and package.json"

#USAGE arg "<version>" help="pnpm version to upgrade to"

set -e

VERSION="${usage_version}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "invalid semver: $VERSION" >&2; exit 1; }
ROOT="$(git rev-parse --show-toplevel)"

if ! command -v jq >/dev/null; then
  echo "jq is not installed" >&2
  exit 1
fi

mise use --pin "pnpm@${VERSION}"

while IFS= read -r rel; do
  pkgPath="$ROOT/$rel"
  if jq -e '.packageManager // "" | startswith("pnpm@")' "$pkgPath" >/dev/null 2>&1; then
    if jq -e --arg v "pnpm@${VERSION}" '.packageManager == $v' "$pkgPath" >/dev/null 2>&1; then
      echo "no change $rel (already at pnpm@${VERSION})"
    else
      tmp=$(mktemp "${pkgPath}.XXXXXX")
      if jq --arg v "pnpm@${VERSION}" '.packageManager = $v' "$pkgPath" > "$tmp"; then
        mv "$tmp" "$pkgPath"
        echo "updated $rel"
      else
        rm -f "$tmp"
        exit 1
      fi
    fi
  fi
done < <(git -C "$ROOT" ls-files '*/package.json' 'package.json')
