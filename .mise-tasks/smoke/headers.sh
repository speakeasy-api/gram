#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE description="Check that a Gram host returns every expected security response header"
#USAGE arg "<target>" help="Host to check: dev, prod, or a full URL" default="dev"

# Headers reach the browser from two layers: this dashboard nginx config,
# which sets the cross-origin headers, and the shared edge configuration,
# which sets the rest. Only a live request proves both survived, so check a
# running host rather than a file.

set -euo pipefail

target="${1:-dev}"

case "$target" in
  dev) url="https://dev.getgram.ai/" ;;
  prod) url="https://app.getgram.ai/" ;;
  *) url="$target" ;;
esac

# Name and exact value. Every one of these is a fixed string we control.
exact=(
  "cross-origin-resource-policy|same-origin"
  "cross-origin-opener-policy|same-origin"
  "x-permitted-cross-domain-policies|none"
  "x-content-type-options|nosniff"
  "x-frame-options|deny"
  "referrer-policy|strict-origin-when-cross-origin"
  "x-xss-protection|1; mode=block"
)

# Values that change, or are too long to pin. Presence is the assertion.
present=(
  "strict-transport-security"
  "content-security-policy"
  "permissions-policy"
  "reporting-endpoints"
)

response="$(curl --silent --show-error --output /dev/null --dump-header - \
  --connect-timeout 5 --max-time 20 "$url")"

status="$(printf '%s\n' "$response" | head -n1 | tr -d '\r')"
headers="$(printf '%s\n' "$response" | tr -d '\r' | tr '[:upper:]' '[:lower:]')"

echo "GET $url"
echo "$status"
echo

failures=0

header_value() {
  printf '%s\n' "$headers" | sed -n "s/^$1: *//p" | head -n1
}

for entry in "${exact[@]}"; do
  name="${entry%%|*}"
  want="${entry#*|}"
  got="$(header_value "$name")"

  if [[ "$got" == "$want" ]]; then
    echo "ok       $name: $got"
  elif [[ -z "$got" ]]; then
    echo "MISSING  $name (want \"$want\")"
    failures=$((failures + 1))
  else
    echo "WRONG    $name: \"$got\" (want \"$want\")"
    failures=$((failures + 1))
  fi
done

for name in "${present[@]}"; do
  got="$(header_value "$name")"

  if [[ -n "$got" ]]; then
    echo "ok       $name (present, ${#got} chars)"
  else
    echo "MISSING  $name"
    failures=$((failures + 1))
  fi
done

echo

# Clear-Site-Data is absent here on purpose. It ships on the logout response
# only. On GET / the "cookies" directive would delete the session cookie on
# every page load and sign the user out. See AIS-551.
if [[ -n "$(header_value "clear-site-data")" ]]; then
  echo "WRONG    clear-site-data is set on GET /, which signs users out on every page load"
  failures=$((failures + 1))
fi

if [[ "$failures" -gt 0 ]]; then
  echo "$failures header check(s) failed"
  exit 1
fi

echo "all header checks passed"
