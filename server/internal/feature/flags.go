package feature

type Flag string

const (
	// FlagChannels gates the Channels rollout for an organization.
	FlagChannels Flag = "channels"
	// FlagToolApprovals gates creating require_approval risk policies (tool
	// approvals) and the dashboard's approval wizard and Tool approvals tab.
	// Evaluated server-side on policy create and update and in the dashboard;
	// targeted by PostHog organization group (org slug). Fails closed. Existing
	// approval policies keep enforcing and their requests stay decidable when
	// the flag is off. Removed once tool approvals are GA.
	FlagToolApprovals Flag = "risk-tool-approvals"
	// FlagBudgets gates the Budgets (spend control) rollout end to end with
	// one key: the dashboard hides the Budgets tab on the Costs page behind
	// it, and the background spend-rule evaluator skips organizations
	// without it — no warning/breach events are recorded and the hooks spend
	// gate snapshot is cleared, so enforcement never blocks an org that
	// cannot see the feature. Targeted by PostHog organization group (org
	// slug), the same way the dashboard evaluates it.
	FlagBudgets Flag = "gram-budgets"
	// FlagAccessRequests gates stored access requests: requestAccess records
	// a request an admin can approve as a temporary grant or deny, from the
	// dashboard or a Slack DM, and refuses scopes that cannot be given
	// temporarily. Evaluated server-side in requestAccess and in the
	// dashboard; targeted by PostHog organization group (org slug). Fails
	// closed: requestAccess then only emails the admins, as before, and the
	// request listing answers not found. Requests already stored stay
	// decidable and withdrawable. Removed once access requests are GA.
	FlagAccessRequests Flag = "access-requests"
	// FlagRiskEnforcementPubsub routes applicable realtime scanner lanes over Pub/Sub.
	FlagRiskEnforcementPubsub Flag = "risk-enforcement-pubsub"
	// FlagRiskEnforcementMaxContentBytes overrides the realtime enforcement
	// truncation limit globally. Evaluated server-side with no targeting. The
	// payload is {"max_content_bytes": <int>}; a missing or invalid payload
	// keeps the default limit.
	FlagRiskEnforcementMaxContentBytes Flag = "risk-enforcement-max-content-bytes"

	// FlagDeviceLevelCoverage switches device-agent coverage from matching a
	// device's assigned-user email against user-keyed heartbeats to matching
	// its hardware serial against device-keyed ones, falling back to email
	// when no serial match exists. Gated because the flip is visible: an org
	// whose agents predate hardware reporting sees the same numbers, but one
	// mid-upgrade sees devices move between buckets as serials arrive.
	// Targeted by PostHog organization group (org slug), like FlagBudgets.
	// Removed once device-level matching is GA.
	FlagDeviceLevelCoverage Flag = "device-level-coverage"

	// FlagPlatformMCPRiskMutations is the exact-project kill switch for risk
	// policy and exclusion writes exposed through Platform MCP. It is evaluated
	// at invocation time and fails closed when absent, disabled, or indeterminate.
	FlagPlatformMCPRiskMutations Flag = "platform-mcp-risk-mutations"
	// FlagPlatformMCPPluginAssignmentMutations is the exact-project kill switch for
	// replacing a plugin's complete audience assignment set through Platform MCP.
	// It is evaluated at invocation time and fails closed.
	FlagPlatformMCPPluginAssignmentMutations Flag = "platform-mcp-plugin-assignment-mutations"
	// FlagGatewayPluginMembership gates new gateway attachments to plugins.
	FlagGatewayPluginMembership Flag = "gateway-plugin-membership"
	// FlagShadowMCPAutoLink links a Shadow MCP server automatically to the
	// project MCP server that proxies its URL, so direct calls are sent there
	// without an administrator linking the two. Evaluated server-side by the
	// hook guard and the Shadow MCP inventory; targeted by PostHog
	// organization group (org slug) or project slug group. Fails closed: off,
	// only links an administrator made redirect, as before. Removed once
	// automatic links are GA.
	FlagShadowMCPAutoLink Flag = "shadow-mcp-auto-link"
	// FlagStoredGatewayMachineAccess lets admitted agents and workloads use
	// stored gateways. The server evaluates it per organization and fails closed.
	FlagStoredGatewayMachineAccess Flag = "stored-gateway-machine-access"
	// FlagProjectGatewayMachineAccess lets admitted agents and workloads use
	// project gateways. The server evaluates it per organization and fails closed.
	FlagProjectGatewayMachineAccess Flag = "project-gateway-machine-access"
	// FlagPlatformMCPAccessRoleMutations is the exact-project kill switch for
	// creating and updating custom MCP-only access roles through Platform MCP.
	// It is evaluated at invocation time and fails closed.
	FlagPlatformMCPAccessRoleMutations Flag = "platform-mcp-access-role-mutations"
	// FlagPlatformMCPShadowAccessDecisions is the exact-project kill switch for
	// approval decisions exposed through Platform MCP. It is evaluated at
	// invocation time and fails closed independently of the dashboard workflow.
	FlagPlatformMCPShadowAccessDecisions Flag = "platform-mcp-shadow-access-decisions"

	// FlagAssistantPlatformMCP grants a project's managed (dashboard)
	// assistant the Platform MCP read toolset — the "platform" platform
	// toolset re-serving the Platform MCP read tools over the assistant
	// runtime channel. Targeted by PostHog organization group (org slug),
	// like FlagBudgets. Evaluated server-side only; removed once the toolset
	// is GA.
	FlagAssistantPlatformMCP Flag = "assistant-platform-mcp"
	// FlagExplore gates Explore and the Platform MCP analytics tools. The key
	// matches the dashboard's page-level flag so one PostHog flag controls
	// both surfaces; targeted by organization group (org slug), like
	// FlagBudgets. A rollout gate, not authorization. Removed once Explore is GA.
	FlagExplore Flag = "gram-explore"
	// FlagMCPConnectionHealth gates the MCP connection health reads (Connection
	// Logs, sign-in summary, session activity, denied clients). Evaluated
	// server-side, where every mcpConnectionHealth method answers not found
	// without it, and by the dashboard under the same key. Target by
	// organization group (org slug) or by person "email" (lowercase); API keys
	// never match a person, and on the server nobody does without local
	// evaluation (PostHog personal API key). Switch to people only after this server is fully
	// deployed, and back before rolling it back; servers follow within about a
	// minute, so check a listed person and an unlisted member of a previously
	// enabled organization. Fails closed. A rollout gate, not authorization.
	// Removed once the pages are GA.
	FlagMCPConnectionHealth Flag = "mcp-connection-health"
	// FlagIntentPolicies gates the Intent-based policies page and its
	// intentPolicies endpoints (guardrail, analyze, upload). Evaluated on the
	// server and in the dashboard under the same key; targeted per person by
	// "email" (lowercase). Only signed-in sessions match, and the server only
	// with local evaluation (PostHog personal API key). Fails closed. A rollout gate on top of the
	// endpoints' chat and project RBAC checks, not authorization. Removed once
	// the feature is GA.
	FlagIntentPolicies Flag = "intent-policies"
	// FlagRiskLLMAnalyzer selects the engine behind an organization's secret,
	// PII, prompt injection and destructive tool call detection on both the
	// realtime enforcement lane and the batch flag lane. It is multivariate:
	// VariantRiskLLMOff keeps the gitleaks, Presidio, prompt-injection and
	// destructive-tool analyzers; VariantRiskLLMShadow keeps them enforcing
	// and additionally runs the fine-tuned risk model on the same traffic,
	// recording its findings for comparison without ever enforcing them;
	// VariantRiskLLMLLM replaces the legacy analyzers with the model.
	// Policies keep their configured sources; the variant swaps the engine
	// behind them. Evaluated server-side with distinct id = organization id
	// and the organization / slug groups (OrgProjectGroups), so a release
	// condition selects organizations by the organization group key (org
	// slug) or by distinct id (org id). Transition rule while the PostHog key
	// is still boolean: an empty variant with the boolean read true resolves
	// to VariantRiskLLMLLM (RiskLLMAnalyzerVariant). Fails safe to
	// VariantRiskLLMOff when the flag is off, absent, unrecognized or the
	// provider errors. Removed at GA, or promoted to a productfeatures
	// entitlement if the analyzer becomes a sold capability.
	FlagRiskLLMAnalyzer Flag = "gram-risk-llm-analyzer"

	// FlagAssistantOAuthCIMD gates outbound Client ID Metadata Document
	// (CIMD) support for assistant MCP OAuth clients. When on, and the
	// upstream authorization server advertises
	// client_id_metadata_document_supported, assistants publish a stable
	// metadata document and send its URL as client_id instead of dynamic
	// client registration. Off (the default) keeps the DCR reuse path.
	// Speakeasy-hosted issuers admit assistant documents ahead of their CIMD
	// admission policy, so enabling this never depends on a catalog preset
	// or a per-issuer custom URL. Targeted by PostHog organization group
	// (org slug). Removed once CIMD is GA.
	FlagAssistantOAuthCIMD Flag = "assistant-oauth-cimd"

	// FlagMCPResearchKill is the research kill switch: affirmatively on means
	// no research runs anywhere. Fails closed: research must not run while
	// the state of its stop control is unknown.
	FlagMCPResearchKill Flag = "gram-mcp-research-kill"

	// FlagSkillsOverMCP gates serving managed skills over MCP (the
	// `io.modelcontextprotocol/skills` extension) end to end with one key:
	// the hosted and platform toolset surfaces advertise the extension,
	// answer `skills/list` and `skills/get`, and serve `skill://` resources
	// only for organizations with it, and the dashboard shows the matching
	// MCP serving hints behind the same key. Off, the extension is absent
	// from capabilities, its methods answer method-not-found, and
	// `skill://` reads are not found. Targeted by PostHog
	// organization group (org slug), like FlagBudgets. A rollout gate while
	// client support matures; removed once the extension is GA.
	FlagSkillsOverMCP Flag = "skills-over-mcp"

	// FlagHooksRollout is the legacy control for the phased rollout of new
	// observability (hooks) plugin generator versions, superseded by the
	// rollout pins staff set in the admin dashboard (package hooksrollout). It
	// is consulted via its PAYLOAD, not its boolean state: JSON {"version": N}
	// naming the highest hooksGeneratorVersion the matched org is cleared to
	// receive. The plugin publisher reads it only for orgs no admin pin covers,
	// which is every non-canary org without an override until a platform-wide
	// default pin is set. Evaluated server-side with the org groups; fails closed. Remove it
	// once the default pin is set in production.
	FlagHooksRollout Flag = "hooks-rollout"

	// FlagSignInEnforcement turns on, per organization (keyed by its id),
	// linking MCP grants to sign-ins, sign-in evidence lookups, sign out
	// everywhere, member sign-in revocation and revocation on deprovisioning.
	// Enable it only once every pod runs the code that honours it. It is
	// evaluated with the organization id as the distinct id and no groups, so
	// the PostHog flag must be person-aggregated and match distinct_id in a
	// list of organization ids; a group-aggregated flag never matches.
	FlagSignInEnforcement Flag = "sign-in-enforcement"

	// FlagMCPConsentAccountSwitch lets a person whose personal account company
	// sign-in refused switch to their company account from the MCP connect
	// page: the offer and the switch action, the spent-page message, and the
	// browser binding /authorize sets on the WorkOS sign-in path. Evaluated
	// locally for the organization that owns the MCP server, with its id as
	// the distinct id and no groups, so the PostHog flag must be
	// person-aggregated and match distinct_id in a list of organization ids.
	// Off, missing or failing, the connect page behaves as before. Removed
	// once the switch is GA.
	FlagMCPConsentAccountSwitch Flag = "mcp-consent-account-switch"

	// FlagEMAProviders gates changing a vendor's EMA provider order while the
	// Speakeasy issuer is dogfooded. Targeted by PostHog organization group.
	FlagEMAProviders Flag = "ema-providers"

	// FlagInternalSessionIssuer gates an organization's internal session issuer
	// while it rolls out: enabling it, signing with it and its Platform MCP tool.
	// Server-evaluated, targeted by the organization group.
	FlagInternalSessionIssuer Flag = "internal-session-issuer"

	// FlagInternalSessionIssuerSigningPaused pauses every internal session
	// issuer signature without a redeploy, for incident response. Discovery
	// keeps serving. Server-evaluated; target everyone to pause the platform,
	// or an organization group to pause one organization.
	FlagInternalSessionIssuerSigningPaused Flag = "internal-session-issuer-signing-paused"

	// FlagMandatoryAuthzShadow evaluates the mandatory server and auth client
	// check beside today's decision for an organization's MCP requests and
	// records where they disagree, without changing any response.
	// Targeted locally with the organization ID as the distinct ID.
	FlagMandatoryAuthzShadow Flag = "mandatory-authz-shadow"

	// FlagUpstreamIssuerFromAssociations makes an MCP server's upstream
	// clients the source of its upstream issuer for an organization: the one
	// provider among the server's live client associations routes its
	// credentials and is hashed into its consent evidence, in place of the
	// denormalized mcp_servers.remote_session_issuer_id. Off, the column
	// keeps serving and is the rollback. Either way both values are compared
	// wherever a request reads the server's associations and no credential
	// choice governs; elsewhere the request is recorded as a governing choice
	// or as not having read the associations, and keeps the column.
	// Targeted locally with the organization ID as the distinct ID.
	FlagUpstreamIssuerFromAssociations Flag = "upstream-issuer-from-associations"

	// FlagAuthClientUpstreamSelectorWrites lets an organization write grants
	// whose selector names upstream_client: such grants, connection owner
	// blocks, agent keys copying them and the attachment backfill. Replicas
	// older than the dimension drop these restrictions and their policy edits
	// do not protect owner blocks, so enable it only once every replica reads
	// them; that release is then the rollback floor. Off, unavailable or
	// failing, no such grant is written; reading and enforcing existing ones,
	// and protecting existing blocks, never depend on it. Targeted locally with the organization ID as the distinct ID.
	FlagAuthClientUpstreamSelectorWrites Flag = "auth-client-upstream-selector-writes"

	// FlagPlatformMCPDiscoveryMode picks the tool surface a Platform MCP
	// connection is served: the full flat catalogue, or a small fixed surface
	// for hosts with a tool budget (see the PlatformMCPDiscovery variants).
	// Evaluated server-side and locally on every Platform MCP request, with the
	// organization ID as distinct ID and the person properties "harness" (the
	// aivendors product ID of the connection's verified CIMD client, or
	// "unknown") and "organization_id". Release conditions match on those
	// person properties, so a harness joins or leaves the treatment by editing
	// a condition. Fails closed to the full catalogue. ChatGPT always gets the
	// full catalogue, because the ChatGPT app directory rejects tools that run
	// other tools by name.
	FlagPlatformMCPDiscoveryMode Flag = "platform-mcp-discovery-mode"
)

// Variants of FlagPlatformMCPDiscoveryMode. Anything else — no variant, an
// unrecognized key, an unavailable provider, or an evaluation error — serves
// the full flat catalogue, which is the pre-rollout behaviour.
const (
	// VariantPlatformMCPDiscoveryProgressive serves the search, describe and
	// call meta-tools instead of the catalogue.
	VariantPlatformMCPDiscoveryProgressive Variant = "progressive"
	// VariantPlatformMCPDiscoveryCodeMode adds the execute tool to the
	// progressive surface when the organization can use Code Mode.
	VariantPlatformMCPDiscoveryCodeMode Variant = "code_mode"
)

// Variants of FlagAssistantPlatformMCP. Anything else — no variant, an
// unrecognized key, an unavailable provider, or an evaluation error — resolves
// to VariantAssistantToolsLegacy, which is the pre-rollout behaviour, so a
// PostHog outage can never strip the managed assistant's tools.
const (
	// VariantAssistantToolsLegacy serves the managed assistant the
	// "managed-assistant" platform toolset (logs, chats, users, risk,
	// deployments, skills, plugins, docs, changelog).
	VariantAssistantToolsLegacy Variant = "legacy"
	// VariantAssistantToolsPlatformMCP serves the managed assistant the
	// "platform" toolset — the Platform MCP read tools — INSTEAD of the
	// legacy toolset, not in addition to it.
	VariantAssistantToolsPlatformMCP Variant = "platformmcp"
)

// AssistantToolsVariant normalizes a resolved variant to one of the two known
// keys, collapsing everything unrecognized onto the legacy default. Both the
// attach path (assistants service) and the serve path (mcp service) must agree
// on this mapping or a toolset would be attached and then 404 at request time.
func AssistantToolsVariant(variant Variant) Variant {
	if variant == VariantAssistantToolsPlatformMCP {
		return VariantAssistantToolsPlatformMCP
	}
	return VariantAssistantToolsLegacy
}

// Variants of FlagRiskLLMAnalyzer, the risk engine mode of an organization.
// Anything else — no variant, an unrecognized key, an unavailable provider,
// or an evaluation error — resolves to VariantRiskLLMOff, which is the
// pre-rollout behaviour, so a PostHog outage never changes which engine
// enforces a policy.
const (
	// VariantRiskLLMOff runs the legacy engines only (gitleaks, Presidio,
	// prompt-injection, destructive-tool, CLI-destructive).
	VariantRiskLLMOff Variant = "off"
	// VariantRiskLLMShadow runs the legacy engines exactly as under
	// VariantRiskLLMOff, and additionally runs the fine-tuned risk model on
	// the same traffic. The model's findings are recorded for engine
	// comparison and never enforced; a model failure never denies.
	VariantRiskLLMShadow Variant = "shadow"
	// VariantRiskLLMLLM replaces the legacy engines with the fine-tuned risk
	// model for every covered policy source. The realtime lane fails closed
	// on a model failure.
	VariantRiskLLMLLM Variant = "llm"
)

// RiskLLMAnalyzerVariant normalizes a resolved FlagRiskLLMAnalyzer variant to
// one of the three known modes. A known variant is returned as is. An empty
// variant with legacyEnabled true — the boolean read of the same key —
// resolves to VariantRiskLLMLLM, which keeps organizations on the boolean
// flag on the model until the PostHog key is switched to multivariate.
// Everything else resolves to VariantRiskLLMOff.
func RiskLLMAnalyzerVariant(variant Variant, legacyEnabled bool) Variant {
	switch variant {
	case VariantRiskLLMOff, VariantRiskLLMShadow, VariantRiskLLMLLM:
		return variant
	case "":
		if legacyEnabled {
			return VariantRiskLLMLLM
		}
		return VariantRiskLLMOff
	default:
		return VariantRiskLLMOff
	}
}
