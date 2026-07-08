import type { Toolset } from "@/lib/toolTypes";

// Pure decision logic for which authentication surface the toolset detail
// page shows. Kept separate from React so the matrix stays unit testable
// without rendering hooks.

export type OAuthParadigm = "external" | "gram" | "proxy";

export function getOAuthParadigm(toolset: Toolset): OAuthParadigm | null {
  if (toolset.externalOauthServer) return "external";
  if (!toolset.oauthProxyServer) return null;
  return toolset.oauthProxyServer.oauthProxyProviders?.[0]?.providerType ===
    "gram"
    ? "gram"
    : "proxy";
}

export type ToolsetAuthSurface =
  // user_session_issuer wired → the shared authentication section (manage
  // identity providers, session duration, active sessions).
  | "manage"
  // legacy OAuth (external / proxy / gram) configured and no
  // user_session_issuer yet → the legacy OAuth UI plus a convert path.
  | "legacy"
  // nothing configured → the shared authentication section in its attach
  // state; the legacy wizard is not reachable.
  | "attach"
  // feature flag off → the pre-user-sessions UI, unchanged.
  | "legacy-only";

export function toolsetAuthSurface({
  flagEnabled,
  userSessionIssuerWired,
  oauthParadigm,
}: {
  flagEnabled: boolean;
  userSessionIssuerWired: boolean;
  oauthParadigm: OAuthParadigm | null;
}): ToolsetAuthSurface {
  if (!flagEnabled) return "legacy-only";
  // A wired user_session_issuer always wins: once linked, the serve path
  // gates on it and any leftover legacy OAuth config is inert.
  if (userSessionIssuerWired) return "manage";
  if (oauthParadigm) return "legacy";
  return "attach";
}

// The convert path offered on the "legacy" surface. Proxy paradigms migrate
// through the wire-user-session-issuer modal (it clones the proxy provider's
// credentials); external OAuth has no credentials to clone, so it converts by
// attaching a fresh identity provider through the attach sheet.
export type ToolsetConvertAction = "wire-modal" | "attach-sheet";

export function toolsetConvertAction(
  oauthParadigm: OAuthParadigm | null,
): ToolsetConvertAction | null {
  switch (oauthParadigm) {
    case "proxy":
    case "gram":
      return "wire-modal";
    case "external":
      return "attach-sheet";
    case null:
      return null;
  }
}

// Whether switching the MCP server to private must be blocked until the
// legacy OAuth config is converted to a user session issuer. The backend
// silently clears external OAuth / OAuth proxy config on any mcp_is_public
// flip (UpdateToolset in server/internal/toolsets/impl.go). Once a user
// session issuer is wired the leftover legacy config is inert, so losing it
// is harmless and the flip goes through. Flag off keeps today's silent
// behavior since no convert path exists there.
export function mustConvertOAuthBeforePrivate({
  flagEnabled,
  mcpIsPublic,
  userSessionIssuerWired,
  oauthParadigm,
}: {
  flagEnabled: boolean;
  mcpIsPublic: boolean;
  userSessionIssuerWired: boolean;
  oauthParadigm: OAuthParadigm | null;
}): boolean {
  return (
    flagEnabled &&
    mcpIsPublic &&
    !userSessionIssuerWired &&
    oauthParadigm !== null
  );
}

// Best-effort issuer URL to seed the attach sheet's discovery when converting
// an external-OAuth toolset: the RFC 8414 `issuer` claim from the stored
// metadata document, when present.
export function externalOauthIssuerUrl(toolset: Toolset): string | undefined {
  const metadata = toolset.externalOauthServer?.metadata as
    | Record<string, unknown>
    | undefined
    | null;
  const issuer = metadata?.["issuer"];
  return typeof issuer === "string" && issuer.trim() ? issuer : undefined;
}
