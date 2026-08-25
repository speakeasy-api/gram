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

lint_input_fingerprint() (
    cd "$current_worktree"

    hash_paths() {
        while IFS= read -r path; do
            if [ -f "$path" ]; then
                printf '%s\n' "$path"
            fi
        done | git hash-object --stdin-paths
    }

    untracked=$(git ls-files --others --exclude-standard)
    special_index=$(
        while IFS= read -r entry; do
            tag=${entry%% *}
            # The pattern carries a leading parenthesis because bash 3.2, which
            # is still the /bin/bash macOS ships, cannot parse a case statement
            # inside $( ) without one and fails the whole script at parse time.
            case "$tag" in
                ([a-z]|S) printf '%s\n' "${entry#? }" ;;
            esac
        done < <(git ls-files -v)
    )
    ignored_go_inputs=$(
        git ls-files --others --ignored --exclude-standard -- \
            '*.go' '*.c' '*.cc' '*.cxx' '*.cpp' '*.m' \
            '*.h' '*.hh' '*.hpp' '*.f' '*.F' '*.for' '*.f90' \
            '*.s' '*.S' '*.sx' '*.swig' '*.swigcxx' '*.syso' \
            'go.mod' 'go.sum' 'go.work' 'go.work.sum'
    )
    go_work=$(go env GOWORK)
    go_env_file=$(go env GOENV)
    # Leading parentheses on the patterns for the bash 3.2 reason above.
    go_config_paths=$(
        case "$go_work" in
            (''|off) ;;
            (*) printf '%s\n%s.sum\n' "$go_work" "$go_work" ;;
        esac
        case "$go_env_file" in
            (''|off) ;;
            (*) printf '%s\n' "$go_env_file" ;;
        esac
    )

    {
        printf 'head\n'
        git rev-parse HEAD
        printf 'staged\n'
        git diff --cached --no-ext-diff --binary | git hash-object --stdin
        printf 'unstaged\n'
        git diff --no-ext-diff --binary | git hash-object --stdin
        printf 'untracked-paths\n%s\nuntracked-contents\n' "$untracked"
        printf '%s\n' "$untracked" | hash_paths
        printf 'special-index-paths\n%s\nspecial-index-contents\n' "$special_index"
        printf '%s\n' "$special_index" | hash_paths
        printf 'ignored-go-paths\n%s\nignored-go-contents\n' "$ignored_go_inputs"
        printf '%s\n' "$ignored_go_inputs" | hash_paths
        printf 'go-config-paths\n%s\ngo-config-contents\n' "$go_config_paths"
        printf '%s\n' "$go_config_paths" | hash_paths
        printf 'go-env\n'
        go env \
            GOOS GOARCH GOAMD64 GOARM64 GOEXPERIMENT GOFLAGS GOVERSION GOROOT \
            CGO_ENABLED CGO_CFLAGS CGO_CPPFLAGS CGO_CXXFLAGS CGO_LDFLAGS \
            CC CXX PKG_CONFIG GOWORK GOENV GOMOD GOMODCACHE GOPATH GOTOOLCHAIN
    } | git hash-object --stdin
)

# golangci-lint includes absolute filenames in its cache keys. Run worktrees
# with the same binary and lint configuration through one stable path so they
# share cache entries. Including the cache key in that path also prevents stale
# reuse after either input changes. The lock prevents another worktree from
# retargeting the symlink while linting is in progress.
stable_worktree="${common_git_dir}/gcl-lint-worktree-${lint_cache_key}"
lint_lock="${stable_worktree}.lock"
lock_start=$(LC_ALL=C ps -p "$$" -o lstart=)
lock_owner="$$:${lock_start}"

acquire_lint_lock() {
    local current_start owner owner_pid owner_start
    while ! ln -s "$lock_owner" "$lint_lock" 2>/dev/null; do
        owner=$(readlink "$lint_lock" 2>/dev/null || true)
        owner_pid=${owner%%:*}
        owner_start=${owner#*:}
        case "$owner_pid" in
            ''|*[!0-9]*) ;;
            *)
                if [ "$owner_start" != "$owner" ]; then
                    current_start=$(LC_ALL=C ps -p "$owner_pid" -o lstart= 2>/dev/null || true)
                    if [ -n "$current_start" ] && [ "$current_start" = "$owner_start" ]; then
                        sleep 1
                        continue
                    fi
                fi
                ;;
        esac
        rm -f "$lint_lock"
    done
}

release_lint_lock() {
    local owner
    owner=$(readlink "$lint_lock" 2>/dev/null || true)
    if [ "$owner" = "$lock_owner" ]; then
        rm -f "$stable_worktree" "$lint_lock"
    fi
}

acquire_lint_lock
trap release_lint_lock EXIT
rm -f "$stable_worktree"
ln -s "$current_worktree" "$stable_worktree"
cd "${stable_worktree}/server"
lint_inputs=$(lint_input_fingerprint)

# A cold golangci-lint run over every server package holds the analysis graph
# for thousands of files at once. Priming the cache from the server binary
# first covers most shared dependencies with a much smaller live set; the full
# run can then reuse those results. Once this cache/config pair has completed,
# skip the prime so warm runs retain their existing fast path.
lint_cache_location=${GOLANGCI_LINT_CACHE:-${XDG_CACHE_HOME:-$HOME}}
lint_warm_key=$(printf '%s\n%s\n' "$lint_cache_key" "$lint_cache_location" | git hash-object --stdin)
lint_warm_marker="${common_git_dir}/gcl-lint-warm-${lint_warm_key}"
lint_success_file="${common_git_dir}/gcl-lint-success-${lint_cache_key}"
lint_success=
if [ -r "$lint_success_file" ]; then
    IFS= read -r lint_success < "$lint_success_file"
fi

# A successful lint is deterministic for the linter, config, worktree, and
# Go build environment fingerprinted above. Avoid starting go list and
# golangci-lint again when another worktree has already checked the exact same
# inputs. --long always runs because its purpose is to print detailed results.
if [ "${usage_long:-false}" != "true" ] && [ "$lint_success" = "$lint_inputs" ]; then
    release_lint_lock
    trap - EXIT
    exit 0
fi

if [ ! -e "$lint_warm_marker" ]; then
    echo "Priming gcl cache..."
    prime_started=$SECONDS
    if ./bin/gcl run \
        --issues-exit-code=0 \
        --show-stats=false \
        --output.text.path=/dev/null \
        .
    then
        echo "Primed gcl cache in $((SECONDS - prime_started))s"
        touch "$lint_warm_marker"
    else
        echo "Failed to prime gcl cache; continuing with full run" >&2
    fi
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

if ./bin/gcl run --max-issues-per-linter=0 "${args[@]}" "${lint_packages[@]}"; then
    lint_status=0
else
    lint_status=$?
fi

if [ "$lint_status" -eq 0 ]; then
    touch "$lint_warm_marker"
    lint_inputs_after=$(lint_input_fingerprint)
    if [ "$lint_inputs_after" = "$lint_inputs" ]; then
        printf '%s\n' "$lint_inputs" > "$lint_success_file"
    fi
fi

release_lint_lock
trap - EXIT
exit "$lint_status"
