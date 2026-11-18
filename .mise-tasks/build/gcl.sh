#!/usr/bin/env bash

#MISE description="Build server/bin/gcl, the patched golangci-lint with the glint plugin compiled in"
#MISE dir="{{ config_root }}/server"
#MISE quiet=true

# gcl is golangci-lint at the version in gcl/version, with the patches in
# gcl/patches applied and the glint plugin compiled in. This reproduces what
# `golangci-lint custom` does, which has no way to apply patches: fetch the
# source, add a blank import of the plugin, point a replace directive at
# ../glint, and build with a version string that names the build's inputs.
#
# The build is skipped when bin/gcl.fingerprint matches the inputs, so callers
# can run this unconditionally. The lint task and CI both build through here,
# so they lint with the same binary.

set -eo pipefail

plugin_module=github.com/speakeasy-api/gram/glint
golangci_module=github.com/golangci/golangci-lint/v2
fingerprint_file=./bin/gcl.fingerprint

version=$(tr -d '[:space:]' < gcl/version)

# The stock golangci-lint from mise lints the other Go trees and is the
# version this build is checked against, so a mise bump that forgets
# gcl/version would otherwise leave the server on the old version.
installed_version=$(golangci-lint version --short)
if [ "${version#v}" != "$installed_version" ]; then
    echo "golangci-lint $installed_version is installed but server/gcl/version builds $version; keep them in sync" >&2
    exit 1
fi

# The inputs are the Go toolchain gcl embeds (a binary built with an older
# go/types cannot check packages that require a newer Go), the glint module,
# the version, the patches, and this script. Only files git would track
# count, so test reports and other ignored artifacts under glint/ do not
# trigger a rebuild, and test fixtures and test files are excluded because
# they never reach the binary.
fingerprint=$(
    cd ..
    {
        go env GOVERSION GOOS GOARCH
        git ls-files --cached --others --exclude-standard -- glint server/gcl .mise-tasks/build/gcl.sh |
            grep -v -e '^glint/testdata/' -e '_test\.go$' |
            LC_ALL=C sort |
            while IFS= read -r path; do
                # A tracked file deleted from the working tree is still an
                # index entry but no longer an input. Paths are hashed next
                # to contents so that a rename also counts as a change.
                if [ -f "$path" ]; then
                    printf '%s\n' "$path"
                    git hash-object "$path"
                fi
            done
    } | git hash-object --stdin
)

built_fingerprint=
if [ -r "$fingerprint_file" ]; then
    IFS= read -r built_fingerprint < "$fingerprint_file"
fi
if [ -x ./bin/gcl ] && [ "$built_fingerprint" = "$fingerprint" ]; then
    exit 0
fi

echo "Building gcl..."
build_started=$SECONDS

work=$(mktemp -d)
trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT

# The module cache verifies the source against go.sum's checksum database,
# and in CI it is already populated by the go-module-cache job.
module_dir=$(go mod download -json "${golangci_module}@${version}" | sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$/\1/p')
if [ -z "$module_dir" ] || [ ! -d "$module_dir" ]; then
    echo "could not download ${golangci_module}@${version}" >&2
    exit 1
fi
src="$work/golangci-lint"
cp -R "$module_dir" "$src"
chmod -R u+w "$src"

# Each patch must apply exactly as written. A patch that no longer applies
# (the usual result of a golangci-lint upgrade) stops the build instead of
# producing a binary without it.
for patch_file in gcl/patches/*.patch; do
    [ -e "$patch_file" ] || continue
    if ! patch --directory="$src" --strip=1 --forward --batch --fuzz=0 --dry-run --silent < "$patch_file" > /dev/null; then
        echo "$patch_file does not apply to golangci-lint $version; update or remove it" >&2
        exit 1
    fi
    patch --directory="$src" --strip=1 --forward --batch --fuzz=0 --silent < "$patch_file"
done

cat > "$src/cmd/golangci-lint/plugins.go" <<EOF
package main

import (
	_ "${plugin_module}"
)
EOF

glint_dir=$(cd ../glint && pwd)

# GOTOOLCHAIN=local pins the build to the Go that mise installs. Without it the
# golangci-lint module can pull a newer toolchain and compile the whole
# dependency tree a second time under it.
export GOTOOLCHAIN=local
go -C "$src" mod edit -replace "${plugin_module}=${glint_dir}"
go -C "$src" mod tidy

# golangci-lint salts its analysis cache with the version string, so it names
# the inputs: results from a build with other patches or another glint are
# never reused.
build_version="${version}-custom-gcl-${fingerprint:0:12}"
build_date=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
go -C "$src" build \
    -ldflags "-s -w -X 'main.version=${build_version}' -X 'main.date=${build_date}'" \
    -o "$work/gcl" \
    ./cmd/golangci-lint

mkdir -p ./bin
mv "$work/gcl" ./bin/gcl
printf '%s\n' "$fingerprint" > "$fingerprint_file"
echo "Built gcl ${build_version} in $((SECONDS - build_started))s"
