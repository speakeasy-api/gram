#!/usr/bin/env bash

#MISE description="Run golangci-lint on the server codebase"
#MISE dir="{{ config_root }}/server"

#USAGE flag "--long" help="Enable more detailed reporting"

# The server is linted with a golangci-lint binary that has the glint plugin
# compiled in (see .custom-gcl.yml). Everything else is stock golangci-lint:
# its cache is shared across worktrees because golangci-lint 2.13.2 and later
# key entries on module-relative paths.

set -eo pipefail

# A cold-cache run keeps every dependency's analysis facts in memory at once
# (about 10 GiB live heap for the server), and with Go's default GOGC the heap
# grows to more than twice that before the collector runs, which pushes even a
# 36 GB laptop into memory compression. A soft limit makes the collector run
# before the heap doubles: it cut a cold run's peak footprint from 27 GiB to
# 11 GiB and made the run faster. Warm runs never get near it. Set GOMEMLIMIT
# in mise.local.toml to override it.
export GOMEMLIMIT="${GOMEMLIMIT:-4GiB}"

args=(--show-stats=false --output.text.print-issued-lines=false)
if [ "${usage_long:-false}" = "true" ]; then
    args=()
fi

# `golangci-lint custom` builds whatever version .custom-gcl.yml names, so a
# mise bump that forgets the file would silently keep linting with the old
# version (and the old cache-key behavior).
expected_version=$(sed -n 's/^version: v\{0,1\}//p' .custom-gcl.yml)
installed_version=$(golangci-lint version --short)
if [ "$expected_version" != "$installed_version" ]; then
    echo "golangci-lint $installed_version is installed but .custom-gcl.yml builds v$expected_version; keep them in sync" >&2
    exit 1
fi

# Rebuild gcl only when its inputs change: the Go toolchain it embeds (a
# binary built with an older go/types cannot check packages that require a
# newer Go), the glint module, and the build configuration. The plugin module
# has its own go.mod, so server dependency bumps do not invalidate the
# binary. Only files git would track count, so test reports and other ignored
# artifacts under glint/ do not trigger a rebuild, and test fixtures and test
# files are excluded because they never reach the binary.
gcl_fingerprint_file=./bin/gcl.fingerprint
gcl_inputs=$(
    cd ..
    {
        go env GOVERSION GOOS GOARCH
        git ls-files --cached --others --exclude-standard -- glint server/.custom-gcl.yml |
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

# golangci-lint keys its cache on a hash of the plugin directory, but that
# hash does not descend into subdirectories, so analyzer changes under
# glint/imports would otherwise be served stale results. Each gcl build
# therefore gets its own cache directory, named after its inputs. Worktrees
# with the same glint share one warm cache, and a worktree that builds a
# different glint does not wipe the cache the others use. A new worktree used
# to clear the shared cache on its first build, and the next run anywhere then
# rebuilt every dependency's facts from source (7 minutes, 27 GiB footprint,
# against 9 seconds warm).
export GOLANGCI_LINT_CACHE="${GOLANGCI_LINT_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/gcl}/glint-${gcl_inputs:0:12}"

gcl_stamp=
if [ -r "$gcl_fingerprint_file" ]; then
    IFS= read -r gcl_stamp < "$gcl_fingerprint_file"
fi

if [ ! -x ./bin/gcl ] || [ "$gcl_stamp" != "$gcl_inputs" ]; then
    echo "Building gcl..."
    build_started=$SECONDS
    # GOTOOLCHAIN=local pins the build to the Go that mise installs. Without it
    # the cloned golangci-lint module can pull a newer toolchain and compile
    # the whole dependency tree a second time under it.
    GOTOOLCHAIN=local golangci-lint custom --destination ./bin --name gcl
    printf '%s\n' "$gcl_inputs" > "$gcl_fingerprint_file"
    echo "Built gcl in $((SECONDS - build_started))s"
fi

# Generated packages produce no diagnostics after golangci-lint's generated
# file filter, but making them initial packages more than doubles cold runtime.
package_dirs=$(go list -f '{{.Dir}}' ./...)
server_prefix="${PWD}/"
lint_packages=(.)
lint_roots=' '
while IFS= read -r package_dir; do
    case "$package_dir" in
        "$PWD/gen"|"$PWD/gen/"*|"$PWD") ;;
        "$PWD"/*)
            relative_package=${package_dir#"$server_prefix"}
            package_root=${relative_package%%/*}
            case "$lint_roots" in
                *" $package_root "*) ;;
                *)
                    lint_packages+=("./${package_root}/...")
                    lint_roots="${lint_roots}${package_root} "
                    ;;
            esac
            ;;
        *)
            echo "Unexpected server package path: $package_dir" >&2
            exit 1
            ;;
    esac
done <<< "$package_dirs"

exec ./bin/gcl run --max-issues-per-linter=0 "${args[@]}" "${lint_packages[@]}"
