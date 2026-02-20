#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Generate Posting collection from server OpenAPI spec"

set -e

if openapi spec upgrade --version 3.1.0 server/gen/http/openapi3.yaml gram.posting.yaml 2>openapi-upgrade.errors.log; then
    echo "OpenAPI spec upgraded successfully."
else
    echo "Failed to upgrade OpenAPI spec. Check openapi-upgrade.errors.log for details."
    exit 1
fi

mkdir -p posting
uvx posting import --type openapi --output posting/generated gram.posting.yaml
rm -f localhost_80.env

# Post-process all generated YAML files:
# 1. Set Gram-Key header value to ${GRAM_KEY}
# 2. Set Gram-Project header value to ${GRAM_PROJECT}
# 3. Delete Gram-Session headers entirely
find posting -name '*.yaml' -print0 | while IFS= read -r -d '' file; do
    # shellcheck disable=SC2016
    yq -i '(.headers[] | select(.name == "Gram-Key")).value = "${GRAM_KEY}"' "$file"
    # shellcheck disable=SC2016
    yq -i '(.headers[] | select(.name == "Gram-Project")).value = "${GRAM_PROJECT}"' "$file"
    yq -i 'del(.headers[] | select(.name == "Gram-Session"))' "$file"
done


touch posting.env
if ! grep -q '^BASE_URL=' posting.env; then
    echo 'BASE_URL=http://localhost:8080' >> posting.env
fi
if ! grep -q '^GRAM_KEY=' posting.env; then
    echo 'GRAM_KEY=' >> posting.env
fi
if ! grep -q '^GRAM_PROJECT=' posting.env; then
    echo 'GRAM_PROJECT=default' >> posting.env
fi
if [ -n "$NODE_EXTRA_CA_CERTS" ] && ! grep -q '^POSTING_SSL__CA_BUNDLE=' posting.env; then
    echo "POSTING_SSL__CA_BUNDLE=$NODE_EXTRA_CA_CERTS" >> posting.env
fi

echo
echo -e "\033[32mFinished generating Posting collection and environment file.\nRemember to set GRAM_KEY and GRAM_PROJECT in posting.env before using the collection.\033[0m"