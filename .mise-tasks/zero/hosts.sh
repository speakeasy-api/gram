#!/usr/bin/env bash
set -euo pipefail

# MISE description='Ensure setup.localhost is in /etc/hosts for enterprise onboarding dev.'
# MISE hide=true

ENTRY="127.0.0.1  setup.localhost"

if grep -q 'setup\.localhost' /etc/hosts 2>/dev/null; then
    echo "✅ setup.localhost already in /etc/hosts"
    exit 0
fi

echo "Adding setup.localhost to /etc/hosts (requires sudo)"
sudo sh -c "echo '$ENTRY' >> /etc/hosts"
echo "✅ Added setup.localhost to /etc/hosts"
