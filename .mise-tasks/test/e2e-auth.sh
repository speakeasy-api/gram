#!/usr/bin/env bash
#MISE description="Generate Playwright auth state by logging in interactively"
#MISE alias="e2e-auth"

set -e

output_file="${1:-client/dashboard/e2e/.auth-state.json}"
base_url="${PLAYWRIGHT_BASE_URL:-http://localhost:5173}"

echo "Opening browser to generate auth state..."
echo "Log in to the application, then close the browser window."
echo "Auth state will be saved to: $output_file"
echo ""

cd client/dashboard
npx playwright codegen --save-storage="../../$output_file" "$base_url"

echo ""
echo "Auth state saved to: $output_file"
echo ""
echo "Run authenticated tests with:"
echo "  mise test:e2e --auth $output_file"
