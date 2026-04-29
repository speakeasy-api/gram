#!/usr/bin/env bash

#MISE description="Upgrade pnpm version in mise.toml and package.json"

#USAGE arg "<version>" help="pnpm version to upgrade to"

set -e

VERSION="${usage_version}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "invalid semver: $VERSION" >&2; exit 1; }
ROOT="$(git rev-parse --show-toplevel)"

mise use --pin "pnpm@${VERSION}"

REPO_ROOT="$ROOT" PNPM_VERSION="$VERSION" node -e "
const fs = require('fs');
const { execSync } = require('child_process');
const root = process.env.REPO_ROOT;
const version = process.env.PNPM_VERSION;
const files = execSync('git -C ' + JSON.stringify(root) + ' ls-files \"*/package.json\" \"package.json\"').toString().trim().split('\n').filter(Boolean);
for (const rel of files) {
  const pkgPath = root + '/' + rel;
  const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf8'));
  if (!pkg.packageManager || !pkg.packageManager.startsWith('pnpm@')) continue;
  pkg.packageManager = 'pnpm@' + version;
  fs.writeFileSync(pkgPath, JSON.stringify(pkg, null, 2) + '\n');
  console.log('updated ' + rel);
}
"
