#!/usr/bin/env bash

#MISE dir="{{ config_root }}/plog"
#MISE description="Test the plog package with optional coverage generation"

#USAGE flag "--cover" help="Generate coverage report"
#USAGE flag "--html" help="Open coverage report in browser"
#USAGE flag "--update" help="Update golden test files"

set -e

args=("./...")

if [ "${usage_update:-false}" = "true" ]; then
    args=("-update" "${args[@]}")
fi

if [ "${usage_cover:-false}" = "true" ]; then
    args=("-coverprofile=cover.out" "-covermode=atomic" "${args[@]}")
fi

gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- "${args[@]}"
test_exit_code=$?

if [ "${usage_cover:-false}" = "true" ] && [ -f "cover.out" ]; then
    go tool cover -html=cover.out -o cover.html
    echo "Coverage report generated: cover.html"

    if [ "${usage_html:-false}" = "true" ]; then
        if command -v open >/dev/null 2>&1; then
            open cover.html
        elif command -v xdg-open >/dev/null 2>&1; then
            xdg-open cover.html
        else
            echo "Could not open browser automatically. Please open cover.html manually."
        fi
    fi
fi

exit $test_exit_code
