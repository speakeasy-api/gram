#!/usr/bin/env bash

#MISE description="Configure the local gram.local proxy"
#MISE hide=true
#MISE dir="{{ config_root }}"

set -euo pipefail

hosts_file="${HOSTS_FILE:-/etc/hosts}"
local_hostnames=(
  "${GRAM_LOCAL_PROXY_HOST:-gram.local}"
  "idp.gram.local"
  "tunnel.gram.local"
  "app.gram.local"
)

host_exists() {
  local host="$1"
  awk -v host="$host" '
    $1 ~ /^#/ { next }
    {
      for (i = 2; i <= NF; i++) {
        if ($i == host) found = 1
      }
    }
    END { exit found ? 0 : 1 }
  ' "$hosts_file"
}

append_hosts_line() {
  local line="$1"
  local backup="${hosts_file}.gram-backup.$(date +%Y%m%d%H%M%S)"

  if [ -w "$hosts_file" ]; then
    cp "$hosts_file" "$backup"
    printf "\n%s\n" "$line" >>"$hosts_file"
    echo "✅ Added local Gram hostnames to $hosts_file. Backup: $backup"
    return 0
  fi

  if ! command -v sudo >/dev/null 2>&1; then
    echo "⚠️  $hosts_file is not writable and sudo is unavailable." >&2
    return 1
  fi

  if [ ! -t 0 ] && ! sudo -n true 2>/dev/null; then
    echo "⚠️  $hosts_file needs sudo and this shell is non-interactive." >&2
    return 1
  fi

  sudo cp "$hosts_file" "$backup"
  printf "\n%s\n" "$line" | sudo tee -a "$hosts_file" >/dev/null
  echo "✅ Added local Gram hostnames to $hosts_file. Backup: $backup"
}

missing=()
for hostname in "${local_hostnames[@]}"; do
  if ! host_exists "$hostname"; then
    missing+=("$hostname")
  fi
done

if [ "${#missing[@]}" -eq 0 ]; then
  echo "✅ Local Gram hostnames already exist in $hosts_file."
else
  hosts_line="127.0.0.1 ${missing[*]} # Gram local development"
  echo "⏳ Adding missing local Gram hostnames: ${missing[*]}"
  if ! append_hosts_line "$hosts_line"; then
    echo "Add this line manually, then rerun ./zero:" >&2
    echo "  $hosts_line" >&2
    exit 1
  fi
fi

if [ -f mise.local.toml ] && grep -Eq '^[[:space:]]*(GRAM_SERVER_URL|GRAM_SITE_URL)[[:space:]]*=' mise.local.toml; then
  echo "ℹ️ Leaving existing GRAM_SERVER_URL/GRAM_SITE_URL overrides unchanged."
else
  mise set --file mise.local.toml GRAM_SERVER_URL="{{env.GRAM_LOCAL_PROXY_URL}}"
  mise set --file mise.local.toml GRAM_SITE_URL="{{env.GRAM_LOCAL_PROXY_URL}}"
  echo "✅ Set GRAM_SERVER_URL and GRAM_SITE_URL to {{env.GRAM_LOCAL_PROXY_URL}} in mise.local.toml."
fi
