# Run Kit assistants locally

Use this guide to configure a local Speakeasy assistant to run inference and
access project MCP servers and built-in toolsets through the Project Gateway.
It covers the opt-in local runtime (`runtime_version=3`), not production
deployment or migration of legacy chat history. Runtime selection does not
migrate v1/v2 transcripts.

## Prerequisites and stack

Before starting, obtain:

- A configured development checkout and a running Docker-compatible engine.
- A local Kit runtime image built from `agents/ate-runtime/Dockerfile`
  (containing `kit-ate-init` and `kit` release `v0.3.1+`), tagged explicitly
  (for example, `localhost/kit-ate-runtime:dev`).
- A development assistant with an active agent-backed workload identity.
- An enabled HTTPS Project Gateway with one organization-level session issuer
  in shared mode. Enable `project-gateway-machine-access` for the organization.

Built-in Speakeasy toolsets (`assistants`, and `platform`, `speakeasy-slack`, or
`assistant-slack` when granted) are served through the same Project Gateway
entry according to the backing agent's `mcp:connect` grants. Do not substitute a
human token or an inference bearer.

For a fresh worktree, initialize the development environment:

```sh
./zero --agent
mise run wake
mise run seed
```

For an already configured worktree, use `mise run wake`. Build the local Kit
runtime image from this repository using the same `kit-ate-init` + `kit serve --remote-acp`
image contract used in ATE deployments:

```sh
docker build -f agents/ate-runtime/Dockerfile -t localhost/kit-ate-runtime:dev .
```

Use an explicit non-`latest` image tag or a digest. Untagged images and `:latest`
are rejected. Tags can still move: use a digest when immutable code selection is
required. An existing container must match the locally resolved image ID.

Return to the Speakeasy checkout for the following steps. Keep credentials and
concrete tenant identifiers in ignored local configuration, never committed
files or command output.

## Architecture and resource use

Local Kit uses the exact same Kit protocol path as deployed ATE runtimes:

- **One container per thread**, running `/usr/local/bin/kit-ate-init` (loopback
  `:8082`) supervising `/usr/local/bin/kit serve --remote-acp` (loopback `:8081`)
  serving native ACP on `/acp`.
- **Per-thread ACP bearer token** derived via HMAC-SHA256 from the runtime's
  encrypted root secret stored in `assistant_kit_gateway_credentials`.
- **Outbound authentication difference from deployed ATE**: deployed ATE
  sandboxes hold no outbound Speakeasy inference or MCP workload credentials
  (only the inbound per-thread ACP bearer) and rely on the platform egress
  gateway to inject bearer tokens. Local development has no egress proxy, so
  local containers continue to use an externally projected read-only workload
  assertion file with RFC 7523 token exchange (`provider = "speakeasy"` and
  `workload_identity` MCP auth). Startup validation rejects the local Kit
  backend whenever `--environment` is not `local`.
- **Expected local resource use**: each active thread starts one lightweight
  container plus one per-thread `/home/kit` volume (~100–250 MiB RAM while
  running). To stay frugal when testing multiple threads locally, the periodic
  idle sweep automatically stops idle per-thread containers after 5 minutes of
  inactivity while preserving their volumes for fast warm restarts on the next
  message. Retired or stopped runtimes are cleaned up by the runtime reaper.

## Configure this worktree

1. Add or update these values in the existing `[env]` section of ignored
   `mise.local.toml`. Replace the placeholders with your local network and
   workload issuer configuration.

   ```toml
   GRAM_ASSISTANT_RUNTIME_PROVIDER = "local"
   GRAM_ASSISTANT_KIT_IMAGE = "localhost/kit-ate-runtime:dev"
   GRAM_ASSISTANT_KIT_NETWORK = "<ISOLATED_NETWORK>"
   GRAM_ASSISTANT_RUNTIME_LOCAL_CA_FILE = "<ABSOLUTE_CA_BUNDLE_PATH>"
   GRAM_ASSISTANT_KIT_WORKLOAD_ISSUER = "<EXTERNAL_ISSUER_URL>"
   GRAM_ASSISTANT_KIT_WORKLOAD_JWKS_URL = "<EXTERNAL_JWKS_URL>"
   GRAM_ASSISTANT_KIT_WORKLOAD_SUBJECT = "<EXACT_SUBJECT>"
   GRAM_ASSISTANT_KIT_WORKLOAD_ASSERTION_FILE = "<ABSOLUTE_DEDICATED_DIRECTORY>/assertion"
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

   Supply a short-lived assertion through the configured read-only file. The
   external issuer must rotate it before renewal. Use a dedicated host directory
   containing no unrelated files, with owner-only directory permissions (`0700`)
   and assertion-file permissions (`0600`). The host-side issuer must retain these
   permissions during rotation. The directory and every replacement assertion
   must be owned by the host identity that the configured container daemon maps
   to guest UID `1000`; do not assume this is host UID `1000`. Rootless Docker
   normally maps the invoking host owner to container root, which is not enough.
   Configure projection ownership using the daemon's actual user-namespace
   mapping, or use an engine whose supported mapping permits this ownership.
   Do not run Kit as root, disable namespace isolation, or widen permissions.
   Before gateway creation, a disposable probe in the pinned image verifies
   access as `1000:1000` through the actual daemon. It has no network, uses a
   read-only root filesystem and assertion mount, and only opens the file; it
   does not copy or print assertion contents. A failed probe blocks creation
   with ownership guidance. Keep this ownership invariant during rotation.
   The image must provide `/bin/sh` for this check.
   The assertion directory is mounted read-only so atomic token replacement remains
   visible. Each cold startup requires a new external
   subject: previously admitted subjects cannot authorize a new generation.
   Local development uses RFC 7523 workload token exchange (deployed ATE
   environments inject bearer credentials at the platform egress instead); there
   is no secret-authentication fallback.
   Keep the projected file outside the workspace root. The owner
   and agent must have explicit inference permission for the project.

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

Kit does not import earlier transcript. A new Kit session in a conversation with
earlier assistant turns receives a notice in its first prompt that earlier
messages are unavailable, then answers the current message.

## Assign project MCP servers

In the assistant's **Overview → MCP servers** section, check a server to grant
its backing agent direct Connect access. Unchecking removes that direct grant;
the access summary shows whether other rules still allow or block access.
Upstream credentials are configured separately. Test with a private server so
public access doesn't mask the grant change.

Kit connects to the Project Gateway as its single MCP endpoint. The gateway
checks the agent's current access on each discovery request and tool call, so
changing a grant takes effect without a runtime restart or a new chat. An
existing runtime started with an older per-server configuration is automatically
replaced on its next turn.

## Validate inference, tool access, and the transcript

A model turn consumes inference quota and may invoke tools. Use a disposable
conversation and approve the test before submitting it. Choose a read-only task
appropriate for the assigned server, for example:

> Use the Project Gateway to discover tools from the assigned MCP server. Report
> which read-only capabilities are available. Do not change any resource.

Check the following results:

1. Verify the control-plane authenticated `GET /chat/models` contract separately.
   Workload grants do not imply catalog access; configure the model explicitly
   rather than relying on authenticated discovery or model fallback.
   Provision the saved model explicitly and retain authoritative model-allowlist
   enforcement on completion. A model menu or session creation does not prove
   authenticated inference.
2. Kit discovers the assigned MCP server and built-in toolsets through the
   Project Gateway. The gateway authenticates the assistant's workload and
   authorizes the backing agent. An authenticated status alone does not prove
   that tool listing or invocation succeeded.
3. The canonical chat contains the user message, assistant text, assistant
   tool-call records, and the corresponding tool results. Inspect the saved chat,
   not only streamed text or inference request logs.

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

Kit `v0.3.1` persists ACP message IDs for newly written v2 transcripts; older
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

Kit (observed on `v0.2.19`) retains failed MCP initialization as an error. Repeating
`tool_search` or touching an unchanged config file does not retry initialization:
reload compares parsed entries. Repair transport before first discovery. For an
isolated fixture already in this state, a deliberate server-key rename can
invalidate the cached entry without restarting the gateway. Preserve the
original config, keep URL and credentials unchanged, and check that no tool
references depend on the old name. This is fixture recovery, not proof of MCP
credential revocation or a general retry API.

| Symptom                                                                             | Likely cause                                                                                                             | Remedy                                                                                                                                                                                                                                                                                                                   |
| ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Worktree MCP registration fails during setup                                        | Missing or incorrect server URL                                                                                          | Run `mise run git:workmcp --server-url 'https://<WORKTREE_SERVER_HOST>:<PORT>'` with this worktree's HTTPS server URL, not its dashboard URL; rerun the failed setup step.                                                                                                                                               |
| Inference cannot reach the server or TLS validation fails                           | Container-unreachable origin or missing development CA                                                                   | Correct the runtime server origin and CA file. Keep MCP resource and issuer URLs HTTPS.                                                                                                                                                                                                                                  |
| `/chat/models` returns `502` with `model catalog unavailable`                       | Upstream discovery failed, or the composed chat client does not expose catalog discovery                                 | Check the upstream error and ensure the server includes catalog delegation through the agentic chat wrapper. Do not substitute a fabricated model catalog.                                                                                                                                                               |
| MCP authentication fails                                                            | Gateway availability, its shared session issuer, the machine-access flag, or the projected workload assertion is invalid | Check that the Project Gateway is enabled and reachable over HTTPS, its organization session issuer is in shared mode, machine access is enabled for the organization, and the current assertion matches the admitted workload identity. Then check effective Connect access for the backing agent on the member server. |
| MCP is authenticated but discovery reports `subscriptions/listen: Method not found` | Optional subscription capability handling bug in AgentKit 0.11.2                                                         | Treat subscription/discovery failure separately from authentication. Use a runtime build with corrected optional-capability handling before accepting discovery as healthy; changing credentials does not fix the missing method.                                                                                        |
| Tool results appear, but saved assistant text or tool calls are missing             | Adapter treats live chunks as transient or treats every tool update as only a result                                     | Use the chunk-reducing and tool-call-projecting adapter. Recover the resident journal through the guarded projector; do not send another prompt merely to repair saved output.                                                                                                                                           |
| Runtime startup reports saved metadata does not match authority                     | Empty reservation metadata is mishandled, or saved metadata belongs to another runtime                                   | Use the adapter that recognizes the empty reservation sentinel. Investigate nonempty authority mismatches; never clear saved metadata to force reuse.                                                                                                                                                                    |

## Startup files and authority

Provisioning writes private credentials before the gateway starts. Do not print,
copy into transcripts, or commit those files. Fresh process starts require
a fresh externally projected workload assertion; a token recovered from a snapshot
must not be reused. This startup requirement does not establish cold-restore correctness.

Adapter maintainers can find the exact file names, permissions, control-plane chat binding,
projection rules, and restore constraints in the
[Kit assistant adapter reference](../design/kit-assistant-adapter.md).

## Platform MCP parity

Kit reaches both project MCP servers and built-in Speakeasy toolsets through the
single Project Gateway entry using workload authentication, not inference
tokens. Without automatically attached thread context, tools that always need a
conversation advertise a required `thread_id`; Slack self-replies and wake
triggers need it only when `route_replies` is `self` or the trigger definition
is `wake`. An explicit `thread_id` on a tool that changes state requires write
access to the target assistant; the current-thread default does not add that
check. Kit receives the current thread ID in per-turn instructions as a
convenient default. Explicit targets use the existing caller and tool
authorization; selecting another authorized thread does not require
runtime/session attribution. When attached context is available, the existing
automatic behavior and tool schemas are preserved.

Granting or revoking an agent's direct `mcp:connect` access is a
security-sensitive authorization policy change and remains dashboard-only (in
the assistant's **Overview → MCP servers** panel and the Agents access UI);
Platform MCP intentionally exposes only read-only `get_agent_mcp_access` and
directs users to the dashboard for policy changes.

An authorized administrator already uses `select_assistant_runtime` to preview
and confirm the runtime choice for an exact project and assistant. Workload
admission, model bootstrap configuration, and local TLS routing are internal
provisioning details, not new administrator outcomes; no additional Platform MCP
tool is needed for these changes. Keep credential bytes out of tool results.
The existing tool retains project authorization, explicit confirmation,
expected-version checks, and shared settlement admission. Focused provisioning
tests establish bootstrap behavior, not successful runtime readiness.
