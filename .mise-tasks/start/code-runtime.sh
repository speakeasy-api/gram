#!/usr/bin/env bash

#MISE description="Run the native local Code Mode runtime under Pitchfork"
#MISE dir="{{ config_root }}"
#MISE hide=true
#USAGE flag "--url <url>" env="GRAM_CODE_RUNTIME_URL" help="Loopback HTTP address shared with the gateway"
#USAGE flag "--monty-bin <path>" env="GRAM_CODE_RUNNER_MONTY_BIN" help="Pinned subprocess executable"

set -euo pipefail

if [ "${GRAM_ENVIRONMENT:-}" != local ] || [ "${GRAM_CODE_RUNTIME_PROVIDER:-}" != local ]; then
  echo "The native code runtime requires local environment and provider settings." >&2
  exit 1
fi
if [ -z "${GRAM_CODE_RUNTIME_TOKEN:-}" ]; then
  echo "Run mise run zero:code-mode to configure the local runtime token." >&2
  exit 1
fi

listen=$(node --input-type=module -e '
  import { isIP } from "node:net";
  const u = new URL(process.argv[1]);
  const host = u.hostname.replace(/^\[|\]$/g, "");
  if (u.protocol !== "http:" || !isIP(host) ||
      !(host === "::1" || host.startsWith("127.")) ||
      u.username || u.password || u.pathname !== "/" || u.search || u.hash) {
    throw new Error("Code runtime URL must be a plain HTTP literal loopback address.");
  }
  console.log(`${u.hostname}:${u.port || "80"}`);
' "${usage_url:?GRAM_CODE_RUNTIME_URL or --url is required}")

monty_bin="${usage_monty_bin:-$MISE_PROJECT_ROOT/agents/code-runner/target/monty/bin/monty}"
if [ ! -x "$monty_bin" ]; then
  echo "Run mise run zero:code-mode to build the pinned subprocess executable." >&2
  exit 1
fi
mise run build:code-runner
export GRAM_CODE_RUNNER_TOKEN="$GRAM_CODE_RUNTIME_TOKEN"
exec "$MISE_PROJECT_ROOT/agents/code-runner/target/debug/gram-code-runner" \
  --listen "$listen" --monty-bin "$monty_bin"
