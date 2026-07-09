#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin locally (from marketplace)"
#MISE dir="{{ config_root }}"
#USAGE flag "--rm" help="Remove the local Gram hooks Claude plugin from the marketplace after testing"
#USAGE flag "--scope <scope>" {
#USAGE   help "Scope for the marketplace and plugin commands"
#USAGE   default "local"
#USAGE   choices "local" "user" "project"
#USAGE }

set -euo pipefail

scope="${usage_scope:-local}"

install() {
	claude plugin marketplace add --scope "$scope" "$PWD"
	claude plugin install --scope "$scope" gram-hooks@gram
}

uninstall() {
	if claude plugin list --json | jq -e --arg scope "$scope" 'any(.[]; .id == "gram-hooks@gram" and .scope == $scope)' >/dev/null; then
		claude plugin uninstall --scope "$scope" gram-hooks@gram
	else
		echo "Plugin gram-hooks@gram is not installed in scope '$scope', skipping uninstall"
	fi

	if claude plugin marketplace remove --scope "$scope" gram 2>/dev/null; then
		echo "Removed marketplace gram from scope '$scope'"
	else
		echo "Marketplace gram is not registered in scope '$scope', skipping remove"
	fi
}

if [ "${usage_rm:-false}" = "true" ]; then
	uninstall
else
	install
fi
