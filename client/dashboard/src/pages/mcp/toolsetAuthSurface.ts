import type { Toolset } from "@/lib/toolTypes";

/**
 * Pure decision logic for which authentication surface the toolset detail
 * page shows. React-free so the matrix stays unit testable.
 */

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
  // user_session_issuer wired → shared section, manage state.
  | "manage"
  // legacy OAuth configured, unwired → legacy UI plus a convert path.
  | "legacy"
  // nothing configured → shared section, attach state.
  | "attach"
  // flag off → pre-user-sessions UI, unchanged.
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
  // A wired issuer always wins: the serve path gates on it and any leftover
  // legacy OAuth config is inert.
  if (userSessionIssuerWired) return "manage";
  if (oauthParadigm) return "legacy";
  return "attach";
}

/**
 * Convert path offered on the "legacy" surface. Proxy paradigms migrate via
 * the wire modal (it clones the proxy's credentials); external OAuth has none
 * to clone, so it converts by attaching a fresh provider via the attach sheet.
 */
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

/**
 * Whether the public→private flip must be blocked pending OAuth conversion.
 * The backend silently clears external OAuth / OAuth proxy config on any
 * mcp_is_public flip (UpdateToolset in server/internal/toolsets/impl.go).
 * A wired issuer makes leftover config inert, so the flip is safe. Flag off
 * keeps today's silent behavior since no convert path exists there.
 */
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

/**
 * Best-effort issuer URL to seed the attach sheet's discovery: the RFC 8414
 * `issuer` claim from the external server's stored metadata, when present.
 */
export function externalOauthIssuerUrl(toolset: Toolset): string | undefined {
  const metadata = toolset.externalOauthServer?.metadata as
    | Record<string, unknown>
    | undefined
    | null;
  const issuer = metadata?.["issuer"];
  return typeof issuer === "string" && issuer.trim() ? issuer : undefined;
}
