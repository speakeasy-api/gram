/**
 * Allowlist of catalog servers that Gram can fully auto-configure during
 * onboarding — i.e. OAuth servers that support RFC 7591 dynamic client
 * registration (DCR). For these, the distribute flow registers a Speakeasy
 * OAuth client on the fly (see `onboardExternalMcpToUserSessions`), so the user
 * never lands on "OAuth setup required".
 *
 * Servers NOT in this list need manual setup (OAuth 2.0 without DCR → manual
 * client credentials via the OAuth proxy wizard; API-key servers → a secret),
 * so onboarding hides them and points the user to the full catalog instead.
 *
 * ── How this list was generated ──────────────────────────────────────────
 * Source of truth is the PulseMCP catalog (`gram-recommended` tenant). Each
 * server's `_meta["com.pulsemcp/server-version"].remotes[*].authOptions[*]
 * .detail.authorizationServerMetadata.registration_endpoint` is PulseMCP's
 * embedded OAuth discovery result. A NON-EMPTY registration_endpoint means the
 * upstream authorization server supports DCR (OAuth 2.1-style). This was
 * validated against Gram's own deploy-time resolution
 * (`external_mcp_tool_definitions.oauth_version`): zero false positives.
 *
 * NOTE: do NOT use PulseMCP's `clientRegistration.dynamic.supported` flag — it
 * optimistically reports `true` for providers (Snowflake, Salesforce, Google,
 * Gong) whose AS exposes no registration endpoint, which is the bug this guards
 * against.
 *
 * This is a static snapshot. Regenerate when the recommended catalog changes by
 * re-running the discovery sweep over the PulseMCP list endpoint.
 * Last generated: 2026-06-11.
 */
const AUTO_CONFIGURABLE_SERVER_SPECIFIERS: readonly string[] = [
  "app.linear/linear",
  "co.huggingface/hf-mcp-server",
  "com.amplitude/mcp-server",
  "com.atlassian/atlassian-mcp-server",
  "com.figma.mcp/mcp",
  "com.getguru/mcp-server",
  "com.grain/grain",
  "com.monday/monday.com",
  "com.notion/mcp",
  "com.pulsemcp.mirror/asana-mcp",
  "com.pulsemcp.mirror/canva-mcp",
  "com.pulsemcp.mirror/clickup",
  "com.pulsemcp.mirror/cloudflare",
  "com.pulsemcp.mirror/datadog",
  "com.pulsemcp.mirror/fireflies",
  "com.pulsemcp.mirror/granola",
  "com.pulsemcp.mirror/incident-io",
  "com.pulsemcp.mirror/intercom",
  "com.pulsemcp.mirror/klaviyo",
  "com.pulsemcp.mirror/mercury",
  "com.pulsemcp.mirror/moda",
  "com.pulsemcp.mirror/pulumi-mcp-server",
  "com.pulsemcp.mirror/pylon",
  "com.pulsemcp.mirror/ramp",
  "com.pulsemcp.mirror/tryordinal",
  "com.stripe/mcp",
  "com.supabase/mcp",
  "com.vercel/vercel-mcp",
  "com.webflow/mcp",
  "io.github.getsentry/sentry-mcp",
  "io.github.miroapp/mcp-server",
];

const AUTO_CONFIGURABLE_SERVERS: ReadonlySet<string> = new Set(
  AUTO_CONFIGURABLE_SERVER_SPECIFIERS,
);

/**
 * Whether a catalog server can be distributed during onboarding without any
 * manual auth setup. Keyed on the registry specifier (e.g. "com.vercel/vercel-mcp").
 */
export function isAutoConfigurableServer(registrySpecifier: string): boolean {
  return AUTO_CONFIGURABLE_SERVERS.has(registrySpecifier);
}
