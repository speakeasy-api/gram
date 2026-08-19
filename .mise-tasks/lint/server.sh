#!/usr/bin/env bash

#MISE description="Run golangci-lint on the server codebase"
#MISE dir="{{ config_root }}/server"

#USAGE flag "--long" help="Enable more detailed reporting"

set -eo pipefail

args=(--show-stats=false --output.text.print-issued-lines=false)
if [ "${usage_long:-false}" = "true" ]; then
    args=()
fi

gcl_fingerprint_file=./bin/gcl.fingerprint

gcl_input_fingerprint() (
    cd ..
    paths=$(find glint server/.custom-gcl.yml go.mod go.sum -type f -print | LC_ALL=C sort)
    {
        printf 'paths\n%s\ncontents\n' "$paths"
        printf '%s\n' "$paths" | git hash-object --stdin-paths
    } | git hash-object --stdin
)

gcl_inputs=$(gcl_input_fingerprint)
gcl_stamp=
if [ -r "$gcl_fingerprint_file" ]; then
    IFS= read -r gcl_stamp < "$gcl_fingerprint_file"
fi

gcl_binary=
if [ -x ./bin/gcl ]; then
    gcl_binary=$(git hash-object ./bin/gcl)
fi

if [ "$gcl_stamp" != "${gcl_inputs}:${gcl_binary}" ]; then
    echo "Building gcl..."
    build_started=$SECONDS
    golangci-lint custom --destination ./bin --name gcl
    gcl_binary=$(git hash-object ./bin/gcl)
    gcl_stamp="${gcl_inputs}:${gcl_binary}"
    printf '%s\n' "$gcl_stamp" > "$gcl_fingerprint_file"
    echo "Built gcl in $((SECONDS - build_started))s"
fi

current_worktree=$(git rev-parse --show-toplevel)
common_git_dir=$(cd "$(git rev-parse --git-common-dir)" && pwd)
lint_config=$(git hash-object .golangci.yaml)
lint_cache_key=$(printf '%s\n%s\n' "$gcl_stamp" "$lint_config" | git hash-object --stdin)

# golangci-lint includes absolute filenames in its cache keys. Run worktrees
# with the same binary and lint configuration through one stable path so they
# share cache entries. Including the cache key in that path also prevents stale
# reuse after either input changes. The lock prevents another worktree from
# retargeting the symlink while linting is in progress.
stable_worktree="${common_git_dir}/gcl-lint-worktree-${lint_cache_key}"
lint_lock="${stable_worktree}.lock"

acquire_lint_lock() {
    local owner
    while ! ln -s "$$" "$lint_lock" 2>/dev/null; do
        owner=$(readlink "$lint_lock" 2>/dev/null || true)
        case "$owner" in
            ''|*[!0-9]*) ;;
            *)
                if kill -0 "$owner" 2>/dev/null; then
                    sleep 1
                    continue
                fi
                ;;
        esac
        rm -f "$lint_lock"
    done
}

release_lint_lock() {
    local owner
    owner=$(readlink "$lint_lock" 2>/dev/null || true)
    if [ "$owner" = "$$" ]; then
        rm -f "$stable_worktree" "$lint_lock"
    fi
}

acquire_lint_lock
trap release_lint_lock EXIT
rm -f "$stable_worktree"
ln -s "$current_worktree" "$stable_worktree"
cd "${stable_worktree}/server"

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

if ./bin/gcl run --max-issues-per-linter=0 "${args[@]}" "${lint_packages[@]}"; then
    lint_status=0
else
    lint_status=$?
fi

release_lint_lock
trap - EXIT
exit "$lint_status"
