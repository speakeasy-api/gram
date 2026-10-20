# Run Kit assistants locally

Use this guide to configure a local Speakeasy assistant to run inference and
access an attached MCP resource through Kit. It covers the opt-in local runtime
(`runtime_version=3`), not production deployment or migration of legacy chat
history. Runtime selection does not migrate v1/v2 transcripts.

## Prerequisites and stack

Before starting, obtain:

- A configured development checkout and a running Docker-compatible engine.
- Unmodified Kit commit `2c4a35e` and image
  `localhost/kit-control-plane:2c4a35e`.
- A development assistant with an active agent-backed workload identity and one
  attached, issuer-gated HTTPS `/mcp/<slug>` resource.
- The resource's persisted user-session issuer UUID and advertised OAuth issuer.
  These must identify the same issuer; an unrelated UUID will not work.

Built-in Platform MCP machine authentication is not supported by this local
composition. Do not substitute a human token or an inference bearer.

For a fresh worktree, initialize the development environment:

```sh
./zero --agent
mise run wake
mise run seed
```

For an already configured worktree, use `mise run wake`. If the pinned image is
absent, build it from the matching Kit checkout:

```sh
cd <KIT_WORKTREE>
KIT_IMAGE=localhost/kit-control-plane mise run docker:build -- slim --load \
  --tag localhost/kit-control-plane:2c4a35e
```

Use an explicit non-`latest` image tag or a digest. Untagged images and `:latest`
are rejected. Tags can still move: use a digest when immutable code selection is
required. An existing container must match the locally resolved image ID.

Return to the Speakeasy checkout for the following steps. Keep credentials and
concrete tenant identifiers in ignored local configuration, never committed
files or command output.

## Configure this worktree

1. Add or update these values in the existing `[env]` section of ignored
   `mise.local.toml`. Replace the placeholders with the development resource
   and issuer from the prerequisites.

   ```toml
   GRAM_ASSISTANT_RUNTIME_PROVIDER = "local"
   GRAM_ASSISTANT_KIT_IMAGE = "localhost/kit-control-plane:2c4a35e"
   GRAM_ASSISTANT_KIT_NETWORK = "<ISOLATED_NETWORK>"
   GRAM_ASSISTANT_RUNTIME_LOCAL_CA_FILE = "<ABSOLUTE_CA_BUNDLE_PATH>"
   # Default is client_secret_basic; use private_key_jwt to test ES256 startup.
   GRAM_ASSISTANT_KIT_MCP_AUTH_METHOD = "client_secret_basic"
   GRAM_ASSISTANT_KIT_MCP_RESOURCE = "https://<RESOURCE_HOST>/mcp/<SLUG>"
   GRAM_ASSISTANT_KIT_MCP_ISSUER = "https://<EXPECTED_OAUTH_ISSUER>"
   GRAM_ASSISTANT_KIT_MCP_ISSUER_ID = "<PERSISTED_ISSUER_UUID>"
   # Set when multiple existing root triggers make automatic selection ambiguous:
   # GRAM_ASSISTANT_KIT_ROOT_TRIGGER_ID = "<EXISTING_ROOT_TRIGGER_UUID>"
   ```

2. Configure an isolated container network with a TLS relay whose network-local
   DNS alias is `ai.speakeasy.com`. Kit's native provider uses the fixed origin
   `https://ai.speakeasy.com`; HTTP and insecure TLS are not supported. Do not
   change public DNS or disable certificate verification. The relay must present
   a certificate valid for that hostname and forward to this worktree's API,
   verifying its upstream certificate and hostname as well.

   The mounted CA bundle must retain public roots and include the relay CA and
   any development CA needed for MCP. Use an absolute host path that the container
   engine can mount (`/private/tmp/...`, rather than `/tmp/...`, with Podman on
   macOS). Keep private keys outside the repository. Check certificate expiry
   before reusing a short-lived relay fixture.

   If the persisted MCP issuer uses `https://localhost:<API_PORT>`, that hostname
   must also reach the worktree API from the gateway's network namespace. A
   fixture-only TCP relay bound to loopback in that namespace can forward to
   `host.docker.internal:<API_PORT>` without terminating TLS. Keep verification
   enabled and retain the development CA in the bundle. A relay sharing an old
   gateway's namespace does not configure a newly provisioned gateway.

   Startup provisions Kit's native API-key credential file, not an inference
   bearer or a configurable provider origin. The control plane rotates that key
   and binds it to the runtime. Never print its contents. The saved assistant
   model is also written to Kit's startup configuration; it must remain allowed
   by the authoritative catalog.

   For `private_key_jwt`, trusted startup generates a fresh P-256 key, registers
   only public JWKS against the runtime workload identity, and provisions the
   private PKCS#8 PEM through the mode-0600 credential archive. The MCP config
   contains only its absolute file path and `ES256`. Never reuse a human client
   registration or embed private key bytes in MCP JSON. Changing this setting
   affects future startup; it does not convert credentials in a running guest.

3. Apply the configuration to both services:

   ```sh
   mise exec -- pitchfork restart server
   mise exec -- pitchfork restart worker
   ```

4. Use a new test conversation. Select Kit in the dashboard runtime control, or
   use `select_assistant_runtime`: preview first, confirm the exact project,
   assistant, and target version, then supply the freshly read
   `expected_runtime_version`. Do not bypass busy-work or stale-version conflicts
   with database edits. A saved selection is not proof of runtime readiness.

Preexisting transcript upgrades are unsupported by the pinned runtime. If a turn
fails with `ErrKitUpgradeNoticeUnsupported`, use a new test conversation rather
than importing history or fabricating a successful upgrade notice.

## Update MCP configuration without removing other servers

Before using `_kit/session/update_mcp`, verify that the runtime image supports
its approved additive-upsert contract. The image pin in this guide is unchanged;
it is not evidence that this API has been verified in that image.

- Supply initial servers during session creation or cold restore.
- Through the assistant adapter, omit `mcpServers` on a resident reconnect to
  retain the resident configuration. Explicit lists, including `[]`, are rejected;
  reconnect cannot reconcile the desired server set.
- Use `_kit/session/update_mcp` to add a new client-owned server name or update
  only an existing client-owned name. Omitted names are retained, and `[]` is a
  no-op. Host/plugin name collisions are rejected and built-ins are protected.

For example, updating `inventory` does not remove an already configured
`tickets` server. Do not use omission or an empty list to revoke access: this
contract has neither implicit removal nor a removal API. If revocation requires
removal, fail closed, report the unsupported operation, and block affected work
until supported credential revocation and safe retirement can complete. Do not
claim revocation succeeded while leaving access usable or use replacement resume
as a cancellation workaround.

See the [MCP configuration lifecycle contract](../design/kit-assistant-adapter.md#mcp-configuration-lifecycle)
for adapter behavior at creation, reconnect, and update boundaries.

## Validate inference, tool access, and the transcript

A model turn consumes inference quota and may invoke tools. Use a disposable
conversation and approve the test before submitting it. Choose a read-only task
appropriate for the attached resource, for example:

> Discover the tools available from the attached workload MCP server. Report
> which read-only capabilities are available. Do not change any resource.

Check the following results:

1. Verify the control-plane authenticated `GET /chat/models` contract separately.
   Kit `2c4a35e` fetches public OpenRouter model metadata and has an offline
   fallback; it does not authenticate through Speakeasy's catalog endpoint.
   Provision the saved model explicitly and retain authoritative model-allowlist
   enforcement on completion. A model menu or session creation does not prove
   authenticated inference.
2. Kit discovers the attached MCP resource through its native discovery path.
   The resource authenticates with an agent-only machine session, not a human
   credential. An authenticated status alone does not prove that tool listing
   or invocation succeeded.
3. The canonical chat contains the user message, assistant text, assistant
   tool-call records, and the corresponding tool results. Inspect the saved chat,
   not only streamed text or inference request logs.

### Current-image verification status

The native-key image has passed focused bootstrap, rotation, local composition,
and GKE provisioning tests. Isolated hostname routing and certificate verification
have also been exercised. Real inference succeeded through the native API-key
completion path on unmodified Kit `2c4a35e`; the canonical chat contained one user
row and one assistant row with the requested `KIT_NATIVE_AUTH_OK` marker.
Completion-only principal admission has database-backed regression coverage;
unbound keys and transcript access remain rejected. Native MCP `client_secret`
machine authentication and catalog discovery also succeeded: the intentionally
empty fixture reported `authenticated`, zero tools, and no stale/error flags.
Same-conversation continuation preserved canonical user, assistant, tool-call,
and tool-result rows. A subsequently attached parameterless read-only health tool
was discovered and invoked successfully through native MCP. The canonical chat
contains completed tool results and the final `KIT_NATIVE_TOOL_OK` marker.
This required preserving the admitted agent for workload kill-switch enforcement
and encoding empty tool responses as `content: []`, not `content: [null]`.
A separately provisioned runtime also passed native `private_key_jwt` (ES256)
discovery and health-tool execution, with completed canonical tool records and
`KIT_JWT_VERIFIED_TOOL`. An earlier model reply claimed success after rejected
Runlet programs; that reply is not evidence. Always require completed tool
records, not a marker alone. Resident replay after a control-plane worker restart
also passed: all 18 original canonical rows remained exactly once, followed by
only the new user/assistant pair and `KIT_RESIDENT_REPLAY_OK`. Cold-process restore
still needs verification; request acceptance is not success.

Earlier reports of successful inference and resident replay used the historical
bearer-based integration image. They do not establish compatibility with this
native-key image.

The optional real-image smoke now uses a fresh internal network, a generated
trusted certificate for `ai.speakeasy.com`, and a synthetic local streaming
completion. It verifies native API-key/project headers, ACP authentication,
session creation, prompt execution, authoritative settlement, and close. It
passed against unmodified `2c4a35e`. External routing is blocked; no paid model
is called. This complements, but does not replace, the live inference evidence.

```sh
SPEAKEASY_KIT_IMAGE_SMOKE=1 mise run test:server ./internal/assistants \
  -run '^TestKitLocalExactImageGatewaySmoke$' -count=1
```

`SPEAKEASY_KIT_SMOKE_IMAGE` can select another already-built image. Do not
interpret a synthetic completion as real provider or workload MCP verification.

## Session and transcript rules

Before retrying an interrupted turn, inspect the saved chat and accepted work.
A transport disconnect does not cancel accepted requests, and resubmitting an
ambiguous request can repeat external effects. Transcript replay is not an
exactly-once tool-execution guarantee.

Cancellation requests use a durable mailbox consumed by the worker that owns
the active session connection. A successful cancellation requires that owner to
observe the cancellation terminal, confirm authoritative settlement, and flush
transcript projection before acknowledging it. Request submission or HTTP
acceptance alone does not prove the turn stopped.

`ErrKitInterruptPending` means cancellation is unconfirmed and work may still be
running. A timeout does not remove the queued request. Inspect status and saved
results before retrying or retiring the runtime; do not stop the projector or
use replacement resume to take control away from it. Natural completion is not
reported as successful cancellation.

Focused mailbox and cancellation-state tests have passed. Live foreground
cancellation on Kit `2c4a35e` also passed: after observing a harmless `sleep 90`
tool call, the control-plane interrupt returned `interrupted: true`, the canonical
chat recorded interrupted tool terminals, and the child sleep process was gone.
A second interrupt returned `stopped: false` and `interrupted: false`. This does
not establish cancellation of detached background work.

Resident-session replay and cold-process restore are different operations.
Kit `2c4a35e` persists ACP message IDs for newly written v2 transcripts; older
unstamped transcripts can reconstruct different replay IDs and remain unsafe
for deduplication. Our live cold-process projection still needs verification.
The legacy idle-expiry path deliberately excludes Kit because it lacks an
all-session admission fence. Do not bypass that guard with a direct container
restart: the test needs a supported fenced lifecycle path that retains the same
runtime/session identities and refreshes startup authority. Do not use cold
restart as a transcript-repair shortcut or infer success from resident replay.

Live switch admission on the disposable fixture rejected version 3 → 2 with
HTTP 409 while a foreground tool was active. After confirmed interruption, the
same expected-version update succeeded, readback showed version 2 and paused,
and the Kit guest was stopped. No work was submitted to the legacy runtime.
This covers foreground settlement, not detached background-work retirement.

Before switching away from Kit or removing its configuration, settle and retire
the runtime through the supported lifecycle. Foreground `idle` is not sufficient
proof of settlement. A crashed, stopped, or missing source process cannot certify
settlement of its recorded sessions; switching fails closed even when the database
row is stopped or failed. Do not delete session bindings to bypass this check.
Recover and settle the source through a supported lifecycle before retrying.
Removing `GRAM_ASSISTANT_KIT_IMAGE` does not retire an
existing runtime's authority. See the
[adapter lifecycle contract](../design/kit-assistant-adapter.md#settlement-and-retirement)
when implementing or diagnosing this boundary.

Slack delivery requires a Slack post tool. Plain assistant text is not
automatically posted to Slack.

## Troubleshoot local setup and missing results

Kit `2c4a35e` retains failed MCP initialization as an error. Repeating
`tool_search` or touching an unchanged config file does not retry initialization:
reload compares parsed entries. Repair transport before first discovery. For an
isolated fixture already in this state, a deliberate server-key rename can
invalidate the cached entry without restarting the gateway. Preserve the
original config, keep URL and credentials unchanged, and check that no tool
references depend on the old name. This is fixture recovery, not proof of MCP
credential revocation or a general retry API.

| Symptom                                                                             | Likely cause                                                                             | Remedy                                                                                                                                                                                                                            |
| ----------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Worktree MCP registration fails during setup                                        | Missing or incorrect server URL                                                          | Run `mise run git:workmcp --server-url 'https://<WORKTREE_SERVER_HOST>:<PORT>'` with this worktree's HTTPS server URL, not its dashboard URL; rerun the failed setup step.                                                        |
| Inference cannot reach the server or TLS validation fails                           | Container-unreachable origin or missing development CA                                   | Correct the runtime server origin and CA file. Keep MCP resource and issuer URLs HTTPS.                                                                                                                                           |
| `/chat/models` returns `502` with `model catalog unavailable`                       | Upstream discovery failed, or the composed chat client does not expose catalog discovery | Check the upstream error and ensure the server includes catalog delegation through the agentic chat wrapper. Do not substitute a fabricated model catalog.                                                                        |
| MCP authentication fails                                                            | Resource, OAuth issuer, persisted issuer UUID, or workload identity do not agree         | Recheck the configured attached resource and issuer, then use supported provisioning to issue machine credentials. Do not paste a human token or inference bearer.                                                                |
| MCP is authenticated but discovery reports `subscriptions/listen: Method not found` | Optional subscription capability handling bug in AgentKit 0.11.2                         | Treat subscription/discovery failure separately from authentication. Use a runtime build with corrected optional-capability handling before accepting discovery as healthy; changing credentials does not fix the missing method. |
| Tool results appear, but saved assistant text or tool calls are missing             | Adapter treats live chunks as transient or treats every tool update as only a result     | Use the chunk-reducing and tool-call-projecting adapter. Recover the resident journal through the guarded projector; do not send another prompt merely to repair saved output.                                                    |
| Runtime startup reports saved metadata does not match authority                     | Empty reservation metadata is mishandled, or saved metadata belongs to another runtime   | Use the adapter that recognizes the empty reservation sentinel. Investigate nonempty authority mismatches; never clear saved metadata to force reuse.                                                                             |
| `ErrKitUpgradeNoticeUnsupported`                                                    | The conversation requires a preexisting-transcript upgrade                               | Start a new test conversation. Do not fabricate imported history.                                                                                                                                                                 |

## Startup files and authority

Provisioning writes private credentials before the gateway starts. Do not print,
copy into transcripts, or commit those files. Fresh process starts require
revoked/reissued native inference API keys; a key recovered from a snapshot must
not be reused. This startup requirement does not establish cold-restore correctness.

Adapter maintainers can find the exact file names, permissions, control-plane chat binding,
projection rules, and restore constraints in the
[Kit assistant adapter reference](../design/kit-assistant-adapter.md).

## Platform MCP parity

An authorized administrator already uses `select_assistant_runtime` to preview
and confirm the runtime choice for an exact project and assistant. Native-key
issuance, model bootstrap configuration, and local TLS routing are internal
provisioning details, not new administrator outcomes; no additional Platform MCP
tool is needed for these changes. Keep credential bytes out of tool results.
The existing tool retains project authorization, explicit confirmation,
expected-version checks, and shared settlement admission. Focused provisioning
tests establish bootstrap behavior, not successful runtime readiness.
