import type { Toolset } from "@/lib/toolTypes";

/**
 * Pure decision logic for which authentication surface the toolset detail
 * page shows. React-free so the matrix stays unit testable.
 */

// OAuth proxy has been retired, so external OAuth is the only remaining legacy
// paradigm.
export type OAuthParadigm = "external";

export function getOAuthParadigm(toolset: Toolset): OAuthParadigm | null {
  return toolset.externalOauthServer ? "external" : null;
}

/**
 * Whether a user_session_issuer is wired to the toolset. The read path
 * populates userSessionIssuerSlug together with userSessionIssuerId, but both
 * fields are independently optional in the SDK type, so accepting either
 * keeps every consumer (surface dispatch, private-flip guard) agreeing on
 * wiredness rather than diverging on which field it happens to check.
 */
export function isUserSessionIssuerWired(toolset: Toolset): boolean {
  return !!toolset.userSessionIssuerId || !!toolset.userSessionIssuerSlug;
}

export type ToolsetAuthSurface =
  // user_session_issuer wired → shared section, manage state.
  | "manage"
  // legacy OAuth configured, unwired → legacy UI plus a convert path.
  | "legacy"
  // nothing configured → shared section, attach state.
  | "attach";

export function toolsetAuthSurface({
  userSessionIssuerWired,
  oauthParadigm,
}: {
  userSessionIssuerWired: boolean;
  oauthParadigm: OAuthParadigm | null;
}): ToolsetAuthSurface {
  // A wired issuer always wins: the serve path gates on it and any leftover
  // legacy OAuth config is inert.
  if (userSessionIssuerWired) return "manage";
  if (oauthParadigm) return "legacy";
  return "attach";
}

/**
 * External OAuth is supported for enabled, public toolset servers whose tools
 * advertise OAuth or whose attached external MCP source requires it. Keep this
 * aligned with the legacy OAuth section so the user-sessions surface does not
 * expose a configuration the serve path cannot use.
 */
export function canConfigureExternalOAuth(
  toolset: Toolset,
  externalMcpRequiresOAuth: boolean,
): boolean {
  const hasOAuthAuthorizationCodeFlow =
    (toolset.oauthEnablementMetadata?.oauth2SecurityCount ?? 0) > 0;

  return Boolean(
    toolset.mcpEnabled &&
    toolset.mcpIsPublic &&
    (hasOAuthAuthorizationCodeFlow || externalMcpRequiresOAuth),
  );
}

/**
 * Whether the public→private flip must be blocked pending OAuth conversion.
 * The backend silently clears external OAuth / OAuth proxy config on any
 * mcp_is_public flip (UpdateToolset in server/internal/toolsets/impl.go).
 * A wired issuer makes leftover config inert, so the flip is safe.
 */
export function mustConvertOAuthBeforePrivate({
  mcpIsPublic,
  userSessionIssuerWired,
  oauthParadigm,
}: {
  mcpIsPublic: boolean;
  userSessionIssuerWired: boolean;
  oauthParadigm: OAuthParadigm | null;
}): boolean {
  return mcpIsPublic && !userSessionIssuerWired && oauthParadigm !== null;
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
