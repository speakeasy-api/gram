# Run Kit assistants locally

This runbook covers the opt-in local Kit runtime (`runtime_version=3`). It is not
production deployment guidance. Legacy v1/v2 assistants keep their existing
backend, image, and protocol; setting up Kit does not migrate their transcripts.

## Prerequisites and stack

- Use the integration checkout and a running Docker-compatible engine.
- Use Kit commit `c3cbc0ebfb3c802bd001340d2868448074e0e187`, image
  `localhost/kit-assistant-integration:c3cbc0e`. Earlier integration images lack
  inference association and authoritative settlement support.
- Use a development assistant with an active agent-backed workload identity and
  an attached, issuer-gated MCP resource. The current composition supports one
  configured HTTPS `/mcp/<slug>` resource. Obtain its persisted user-session issuer
  UUID and advertised OAuth issuer; do not substitute an unrelated issuer UUID.
- Built-in Platform MCP machine authentication is **not supported by this
  composition**. Do not substitute a human token or inference bearer for it.

For a fresh worktree, run the normal setup. For an already configured worktree,
start at `wake`:

```sh
./zero --agent
mise run wake
mise run seed
```

If setup cannot register the worktree MCP because its server URL is missing or
wrong, repair it explicitly with this worktree's **HTTPS server URL**, not its
dashboard URL or another worktree's port:

```sh
mise run git:workmcp --server-url 'https://<WORKTREE_SERVER_HOST>:<PORT>'
```

Then rerun any failed setup step. Keep credentials and concrete tenant identifiers
in ignored local configuration, never committed files or command output.

If the pinned image is absent, build it from the matching Kit checkout:

```sh
cd <KIT_WORKTREE>
KIT_IMAGE=localhost/kit-assistant-integration mise run docker:build -- slim --load \
  --tag localhost/kit-assistant-integration:c3cbc0e
```

## Configure this worktree

Add or update these values in the existing `[env]` section of ignored
`mise.local.toml`. Replace placeholders using the development resource above.
The server and worker must both receive the configuration.

```toml
GRAM_ASSISTANT_RUNTIME_PROVIDER = "local"
GRAM_ASSISTANT_KIT_IMAGE = "localhost/kit-assistant-integration:c3cbc0e"
GRAM_ASSISTANT_KIT_MCP_RESOURCE = "https://<RESOURCE_HOST>/mcp/<SLUG>"
GRAM_ASSISTANT_KIT_MCP_ISSUER = "https://<EXPECTED_OAUTH_ISSUER>"
GRAM_ASSISTANT_KIT_MCP_ISSUER_ID = "<PERSISTED_ISSUER_UUID>"
# Set when multiple existing root triggers make automatic selection ambiguous:
# GRAM_ASSISTANT_KIT_ROOT_TRIGGER_ID = "<EXISTING_ROOT_TRIGGER_UUID>"
```

Inference uses the existing local assistant runtime server-origin resolution.
The origin must be reachable from the container; container `localhost` is not
the development host. For intentional local HTTP inference only, set
`GRAM_ASSISTANT_KIT_ALLOW_INSECURE_INFERENCE = "true"`. This does **not** permit
HTTP MCP machine authentication: MCP resource and issuer URLs must remain HTTPS.
For local TLS, configure `GRAM_ASSISTANT_RUNTIME_LOCAL_CA_FILE` with the development
CA bundle as required by the local runtime configuration.

Apply the configuration:

```sh
mise exec -- pitchfork restart server
mise exec -- pitchfork restart worker
```

Use a new test assistant/thread for initial validation. Explicitly select Kit in
the dashboard runtime control, or use `select_assistant_runtime`: preview first,
confirm the exact assistant/project and version, then supply the freshly read
`expected_runtime_version`. A saved selection is not proof of runtime readiness.
Do not bypass busy-work or stale-version conflicts with database edits.

## Startup files and authority

The server provisions files before starting the gateway. Do not manually print,
copy into transcripts, or commit their contents. The private files are owned by
UID/GID 1000 and use mode 0600 under `/home/kit`:

- `.kit/AGENTS.md`: global assistant instructions, loaded before workspace rules.
- `.kit/config.toml`: includes `provider = "speakeasy"`, `credential_store = "file"`,
  `credential_dir = "/home/kit/.kit/credentials"`, and
  `mcp_config = "/home/kit/.kit/credentials/mcp.json"`.
- `.kit/credentials/f5a93b2411bbf44d12df4db53aa064d87685df4fad637f3c61a653f4bc5b200f.json`:
  version-1 inference record with `bearer_token` and matching `origin`.
  **`inference.json` is not a recognized replacement filename.**
- `.kit/credentials/gateway-token`: authenticated gateway bearer.
- `.kit/credentials/mcp.json` and referenced private files: MCP client-credentials
  configuration and machine secrets, not an invented credential-store record.

The approved persistent workspace is `/home/kit/workspace`; ACP `cwd` must match
it exactly. The gateway listens on `0.0.0.0:8081` with explicit wildcard permission
and a credential file; Docker publishes it only on host loopback. Its endpoint is
`/acp/v2`, not a legacy runner endpoint.

Every fresh process start or restore must revoke/remint inference credentials
before startup. Never reuse a bearer recovered from a snapshot or introduce a
per-turn inference-token exchange. The provisioning helper
`PrepareKitStartupBootstrap` performs runtime-row revocation and replacement;
composition also rotates gateway authority and provisions MCP machine auth.

## Session and transcript rules

- Creation/restoration supplies `_meta["ai.speakeasy/gram-chat-id"]` with the existing
  assistant thread's canonical chat UUID. Never substitute an ACP session UUID.
- The existing Speakeasy provider discovers models through authenticated
  `GET /chat/models`; model selection uses ordinary ACP configuration. Inference
  calls `POST /chat/completions` with bearer auth and `Gram-Chat-ID`.
- ACP projection owns the canonical transcript. Trusted inference capture is
  suppressed; stable message IDs make replacement patches replay-safe without
  duplicating insertion usage. Do not import legacy transcript history into Kit.
- Use `kit/session/status` and its authoritative `settled` field while holding
  runtime admission for lifecycle decisions. Foreground `idle` does not mean
  background work is settled. Read failures are not evidence of settlement.
- Require successful durable `session/close` before retiring session authority.
  Transport disconnect only detaches; it does not cancel accepted work.
- Accepted requests can outlive a disconnect. Retrying an ambiguous request can
  repeat external effects. Transcript replay safety is **not** exactly-once tool
  execution; inspect persisted state before deciding whether to resubmit.
- Slack replies remain tool-driven. The existing source instructions require a
  Slack post tool; plain assistant text is not automatically posted to Slack.

## Validation and current limits

The focused local-image smoke has been performed with the pinned image and a
**mock catalog**. It verifies private-file provisioning, gateway authentication,
credential-authenticated discovery, canonical session creation, settlement, and
durable close without making inference calls. Run it again with:

```sh
SPEAKEASY_KIT_IMAGE_SMOKE=1 mise run test:server ./internal/assistants \
  -run '^TestKitLocalExactImageGatewaySmoke$' -count=1 -parallel=1 -p=1
```

**Actual application E2E remains pending acceptance:** validate the real catalog,
model turn, issuer-gated MCP call, dashboard transcript, and restart/resume using
the configured development assistant. The mock smoke does not prove these paths
or Slack delivery.

Do not work around these current limits:

- Runtime-generated context-upgrade notice for a preexisting transcript is
  unsupported by this pinned runtime. Such turns fail with
  `ErrKitUpgradeNoticeUnsupported`; use a new test conversation, not fabricated
  imported history or a fake successful migration notice.
- GKE memory-snapshot restore must restart the gateway/children with freshly
  provisioned credentials. Updating files alone does not invalidate credentials
  cached in a restored process. GKE helpers are not proof that memory restore is
  safe, and this local opt-in does not enable a production GKE deployment.
- Removing `GRAM_ASSISTANT_KIT_IMAGE` leaves legacy composition unchanged, but is
  not a safe retirement procedure for an existing Kit runtime. Settle and retire
  its authority through the supported lifecycle before changing configuration.
