# Run Kit assistants locally

Use this guide to configure a local Speakeasy assistant to run inference and
access an attached MCP resource through Kit. It covers the opt-in local runtime
(`runtime_version=3`), not production deployment or migration of legacy chat
history. Runtime selection does not migrate v1/v2 transcripts.

## Prerequisites and stack

Before starting, obtain:

- A configured development checkout and a running Docker-compatible engine.
- Kit commit `c3cbc0ebfb3c802bd001340d2868448074e0e187` and image
  `localhost/kit-assistant-integration:c3cbc0e`.
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
KIT_IMAGE=localhost/kit-assistant-integration mise run docker:build -- slim --load \
  --tag localhost/kit-assistant-integration:c3cbc0e
```

Return to the Speakeasy checkout for the following steps. Keep credentials and
concrete tenant identifiers in ignored local configuration, never committed
files or command output.

## Configure this worktree

1. Add or update these values in the existing `[env]` section of ignored
   `mise.local.toml`. Replace the placeholders with the development resource
   and issuer from the prerequisites.

   ```toml
   GRAM_ASSISTANT_RUNTIME_PROVIDER = "local"
   GRAM_ASSISTANT_KIT_IMAGE = "localhost/kit-assistant-integration:c3cbc0e"
   GRAM_ASSISTANT_KIT_MCP_RESOURCE = "https://<RESOURCE_HOST>/mcp/<SLUG>"
   GRAM_ASSISTANT_KIT_MCP_ISSUER = "https://<EXPECTED_OAUTH_ISSUER>"
   GRAM_ASSISTANT_KIT_MCP_ISSUER_ID = "<PERSISTED_ISSUER_UUID>"
   # Set when multiple existing root triggers make automatic selection ambiguous:
   # GRAM_ASSISTANT_KIT_ROOT_TRIGGER_ID = "<EXISTING_ROOT_TRIGGER_UUID>"
   ```

2. Check container connectivity. The inference origin uses the local assistant
   runtime's server-origin configuration and must be reachable from the
   container. Container `localhost` is not the development host. For local TLS,
   configure `GRAM_ASSISTANT_RUNTIME_LOCAL_CA_FILE` with the development CA
   bundle. For intentional local HTTP inference only, set
   `GRAM_ASSISTANT_KIT_ALLOW_INSECURE_INFERENCE = "true"`. MCP resource and issuer
   URLs must still use HTTPS.

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

## Validate inference, tool access, and the transcript

A model turn consumes inference quota and may invoke tools. Use a disposable
conversation and approve the test before submitting it. Choose a read-only task
appropriate for the attached resource, for example:

> Discover the tools available from the attached workload MCP server. Report
> which read-only capabilities are available. Do not change any resource.

Check the following results:

1. Model discovery succeeds through authenticated `GET /chat/models`, and the
   assistant uses a model advertised by that catalog.
2. Kit discovers the attached MCP resource through its native discovery path.
   The resource authenticates with an agent-only machine session, not a human
   credential. An authenticated status alone does not prove that tool listing
   or invocation succeeded.
3. The canonical chat contains the user message, assistant text, assistant
   tool-call records, and the corresponding tool results. Inspect the saved chat,
   not only streamed text or inference request logs.

Live inference, agent-only MCP token sessions, native discovery, and recovery of
canonical assistant text/tool rows through resident-session replay have been
verified, including continuation after resident-session replay. The two-turn
validation retained seven canonical rows: two user messages, two assistant text
messages, one assistant tool-call record, and two tool results. The saved replies
contained the exact markers `KIT_E2E_OK` and `KIT_E2E_REPLAY_OK`. This does not
establish cold-restore correctness or successful live cancellation.

For a provisioning-only check without inference calls, run:

```sh
SPEAKEASY_KIT_IMAGE_SMOKE=1 mise run test:server ./internal/assistants \
  -run '^TestKitLocalExactImageGatewaySmoke$' -count=1 -parallel=1 -p=1
```

This smoke uses a mock catalog. It checks private-file provisioning, gateway
access, session creation, settlement, and durable close; it does not prove real
inference, workload tool invocation, continuation, or Slack delivery.

## Session and transcript rules

Before retrying an interrupted turn, inspect the saved chat and accepted work.
A transport disconnect does not cancel accepted requests, and resubmitting an
ambiguous request can repeat external effects. Transcript replay is not an
exactly-once tool-execution guarantee.

Live cancellation is unsupported: for a saved session, the interrupt path returns
`ErrKitInterruptControlUnavailable` without dialing or sending cancellation.
Resuming with replacement would evict the
active transcript projector, so do not use that as a cancellation workaround.
Do not assume a cancel request stopped accepted work; inspect its status and
saved results before retrying or retiring the runtime.

Resident-session replay and cold-process restore are different operations. The
pinned runtime changes message IDs during cold-process replay, so deduplication
across that boundary is unsafe. Do not use a cold restart as a transcript-repair
shortcut or claim successful restore based on resident replay alone.

Before switching away from Kit or removing its configuration, settle and retire
the runtime through the supported lifecycle. Foreground `idle` is not sufficient
proof of settlement. Removing `GRAM_ASSISTANT_KIT_IMAGE` does not retire an
existing runtime's authority. See the
[adapter lifecycle contract](../design/kit-assistant-adapter.md#settlement-and-retirement)
when implementing or diagnosing this boundary.

Slack delivery requires a Slack post tool. Plain assistant text is not
automatically posted to Slack.

## Troubleshoot local setup and missing results

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
revoked/reissued inference credentials; a bearer recovered from a snapshot must
not be reused.

Adapter maintainers can find the exact file names, permissions, ACP metadata,
projection rules, and restore constraints in the
[Kit assistant adapter reference](../design/kit-assistant-adapter.md).
