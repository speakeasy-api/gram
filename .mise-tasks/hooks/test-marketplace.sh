#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin locally (from marketplace)"
#MISE dir="{{ config_root }}"
#USAGE flag "--rm" help="Remove the local Gram hooks Claude plugin from the marketplace after testing"

set -euo pipefail

install() {
	claude plugin marketplace add "$PWD"
	claude plugin install --scope local gram-hooks@gram
}

uninstall() {
	if claude plugin list --json | jq -e 'any(.[]; .id == "gram-hooks@gram")' >/dev/null; then
		claude plugin uninstall --scope local gram-hooks@gram
	else
		echo "Plugin gram-hooks@gram is not installed, skipping uninstall"
	fi

	if claude plugin marketplace list --json | jq -e 'any(.[]; .name == "gram")' >/dev/null; then
		claude plugin marketplace remove gram
	else
		echo "Marketplace gram is not registered, skipping remove"
	fi
}

if [ "${usage_rm:-false}" = "true" ]; then
	uninstall
else
	install
fi
