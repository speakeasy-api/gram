#!/usr/bin/env bash

#MISE description="Print the package patterns that server lint runs on, one per line"
#MISE dir="{{ config_root }}/server"
#MISE quiet=true

# The local lint task and both CI lint jobs read their package list from here,
# so a new top-level package directory is linted everywhere at once.
#
# The list is "." plus "./<dir>/..." for each top-level directory that holds Go
# packages, except gen. Every file under gen carries a generated-code header,
# so golangci-lint drops all of its findings, but analysing its packages as
# lint targets still costs a cold run about 30% more CPU time.

set -eo pipefail

package_dirs=$(go list -f '{{.Dir}}' ./...)
lint_roots=' '
echo .
while IFS= read -r package_dir; do
    case "$package_dir" in
        "$PWD/gen"|"$PWD/gen/"*|"$PWD") ;;
        "$PWD"/*)
            relative_package=${package_dir#"$PWD/"}
            package_root=${relative_package%%/*}
            case "$lint_roots" in
                *" $package_root "*) ;;
                *)
                    echo "./${package_root}/..."
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
