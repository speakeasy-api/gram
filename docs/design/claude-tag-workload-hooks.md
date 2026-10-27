# Claude Tag: hooks, OTEL and MCP from workload identity alone

Ticket: DNO-1440 (split out of DNO-1260). Status: hooks and OTEL path implemented and covered by tests, live Claude Tag verification pending; gateway and Platform MCP decided (both keep refusing workload sessions).

## Goal

A Claude Tag channel gets full observability (hooks and OTEL) and MCP access from its workload identity, with no Speakeasy API key configured anywhere. Its session and tool-log rows name the agent the channel is assigned to, never a human and never "Unknown".

## How Claude Tag authenticates outbound requests

Claude Tag runs Claude in a sandbox. Federated access is handled by Anthropic's Agent Proxy, outside the sandbox ([Connect an authorization server](https://claude.com/docs/claude-tag/admins/federated-access/authorization-server)):

- An admin registers an authorization server: a token endpoint, an optional Issuer URL (the assertion audience; empty means the token endpoint itself), allowed API hosts and, when the token endpoint shares the API host, allowed API paths.
- On a request to an allowed host and path, Agent Proxy sends an RFC 7523 JWT bearer grant with the channel's identity token (`iss` `https://identity.anthropic.com/agents`, `sub` `wimse://identity.anthropic.com/org/<ANTHROPIC_ORG_ID>/agent/<AGENT_ID>`) to the token endpoint, and attaches the returned access token as `Authorization: Bearer`.
- Tokens with `expires_in` between 5 minutes and 1 day are cached per channel. A `401` from the API drops the cached token, and the next request exchanges again.
- Neither the identity token nor the access token is visible inside the sandbox, so nothing in the sandbox, including the Speakeasy plugin's hook binary, can perform the exchange itself.

So the hook binary cannot fetch a token. Agent Proxy has to attach it to the hook binary's and Claude Code's OTEL exporter's own requests, and the binary only has to send them without a key.

## Design

### Exchange

The organization's device-agent token endpoint, `POST <SERVER_URL>/oauth/workload/device-agent/<SPEAKEASY_ORG_ID>/token`, already exchanges a workload assertion for a 15-minute device-agent token. It admits Anthropic's assertions through the same organization-tier trusted issuer, wildcard access rule (`wimse://identity.anthropic.com/org/<ANTHROPIC_ORG_ID>/agent/*`) and agent assignment that the Claude Tag guided setup writes for `/mcp/<slug>`. No server change was needed. A 15-minute token sits inside Agent Proxy's cache window, and an expired one is refused with `401`, which makes Agent Proxy exchange again.

### Hooks and OTEL

`/rpc/hooks.ingest` (and `/rpc/hooks.claude`) read a device-agent token from `Authorization` before `Gram-Key`. `/rpc/hooks.otel/v1/logs` and `/rpc/hooks.otel/v1/metrics` try the device-agent token security first. In every case the workload stays the authenticated actor and the agent it is assigned to is the effective actor:

- sessions are namespaced under the agent and `chats.agent_id` is stamped;
- telemetry rows carry `gram.authorization.actor.{type,id}` for the agent plus the workload issuer and subject;
- self-reported identity (user email, account UUID, provider org ID, device ID) is stripped from rows, and an agent session never seeds the session-metadata cache (`clearAgentAccountIdentity`, `agentSessionView`).

The hook binary gains a `workload_auth` setting (`speakeasy.json` key, or `SPEAKEASY_AI_HOOKS_WORKLOAD_AUTH=1`). When the machine holds no explicit, cached or org key, the binary sends events without `Gram-Key`, so the token Agent Proxy attaches is the only credential. Any key the plugin still carries keeps precedence in the binary, so events keep authenticating if the proxy attaches nothing. On the server the token still wins over the key, so a plugin that carries both is attributed to the agent. A `401` or `403` fails closed and names the platform connection, since no local sign-in can fix a workload identity. Skill content uploads are skipped for a workload credential because that endpoint accepts only API keys, and the server requests the content again on the skill's next activation.

### Claude Tag configuration

1. In Speakeasy, the Claude Tag guided setup (issuer, access rule, agent assignment) is unchanged. Grant the agent `org:hooks_ingest`, bounded to the project its hooks name. Workload admission also bounds every request by the agent owner's live permissions, so the owner needs the same reach.
2. In Claude Tag, connect a second authorization server alongside the MCP one, in the same access bundle:
   - Token endpoint: `<SERVER_URL>/oauth/workload/device-agent/<SPEAKEASY_ORG_ID>/token`.
   - Issuer URL: empty. The device-agent endpoint requires the token endpoint URL as the audience.
   - Allowed API hosts: the Speakeasy host.
   - Allowed API paths: `/rpc/hooks.ingest`, `/rpc/hooks.claude`, `/rpc/hooks.otel`.
3. Keep the hooks paths out of the MCP connection's allowed paths. Otherwise Agent Proxy attaches the MCP session token there, which the hooks endpoints do not accept, and the events fall back to whatever key the plugin carries.
4. Configure the Speakeasy plugin for Claude Tag with `workload_auth` and no `hooks_api_key`. Remove `Gram-Key` from Claude Code's `OTEL_EXPORTER_OTLP_HEADERS` and keep `Gram-Project`.

### Evidence

- `server/internal/mcp/authnchallenge_workloadgrant_deviceagent_claudetag_test.go`: the guided-setup rule admits a channel at the device-agent endpoint, another Anthropic organization and an MCP-audienced assertion are refused, and a token from that exchange sends a hook with no API key that a real hooks service attributes to the agent on `chats.agent_id`. It also covers the token winning over a shared hooks key, logs and metrics admission as the agent, and an expired token answered with `401`.
- `server/internal/hooks/workload_otel_test.go`: a Claude OTEL export sent with a workload token is stored under the agent's session with the agent as actor, keeps none of the export's self-reported identity, including the email of an organization member, and seeds no session metadata.
- `hooks/relay/workload_auth_test.go`: the binary sends keyless in workload mode, enforces denies, keeps an available key, fails closed with the connection named on `401`, stays silent with the setting off, reads the setting from config and environment, replays spooled events under it, and skips skill uploads.

### Open risks

- Not yet verified against a live Claude Tag tenant. Whether Agent Proxy allows two authorization-server connections on one host with disjoint allowed paths is not stated in Anthropic's docs. If it does not, serve hooks from the organization's own Speakeasy host so the two connections have different hosts.
- If the hooks connection is missing or not applied to the channel, events arrive keyless and are acknowledged without processing, the same posture as a machine that never signed in. Enforcement is then off for that channel. The binary cannot tell, because it never sees the token.
- Each authorization server's audience counts toward Claude Tag's limit of 5 registered gateways, so hooks use a second slot.
- Publishing a Claude Tag variant of the plugin with `workload_auth` set is a follow-up. Today it is set through the environment or by editing the plugin's `speakeasy.json`.

## Decision: gateway MCP endpoints

Decision: keep refusing workload sessions at gateway (meta-MCP) endpoints.

- Actor: a workload acting for its assigned agent. Target: an organization or project gateway.
- A workload session carries a ceiling for exactly one MCP resource, or for every MCP server of one user session issuer. `agentAuthorizationTarget` cannot represent a gateway's member set, and runtime authorization is evaluated per member, so a session minted for the gateway could neither be bounded nor revalidated against members added or removed later.
- Claude Tag does not need the gateway for a complete MCP story. A grant with no `resource` mints one issuer-wide session that reaches every MCP server of the shared authorization server at its own `/mcp/<slug>` URL, each checked against the agent's `mcp:connect`.
- Revisit when the agent session policy can name a gateway's member set and recheck it per call. Evidence today: `TestCatalogClaudeTagRule_*` and the issuer-wide workload session tests.

## Decision: Platform MCP

Decision: no change. Platform MCP keeps refusing workload sessions. This is the Platform MCP owner's call (DNO-1386, Q6) and is recorded here as the default until they decide otherwise.

- Actor: a workload acting for its assigned agent. Target: the Platform MCP server.
- `Runtime.authenticate` admits only a principal with a human user, organization, OAuth connection, generation and client, and connection-time admission checks live organization membership. A workload has none of these, so it is refused before any tool runs.
- Platform MCP tools manage organization configuration, and their mutation contracts rely on a human confirming the exact target and impact. An unattended Slack-channel workload cannot give that confirmation, and audit records would attribute configuration changes to an agent with no human in the loop.
- Existing tools: workload trust and credential management remain dashboard and management-API operations (server instructions in `server/internal/platformmcp/tools.go`). No Platform MCP tool is added for the Claude Tag hooks connection either. It is one-time setup done in Anthropic's console, which Platform MCP cannot reach.
