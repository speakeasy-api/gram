import type {
  OAuthProxyProvider,
  Toolset,
} from "@gram/client/models/components";

// Pure helpers that derive migration defaults from a toolset's first
// oauth_proxy_provider. Kept separate from React so the logic stays unit
// testable without rendering hooks.

export const DEFAULT_SESSION_DURATION_HOURS = 24;

export type MigrationDefaults = {
  proxyProvider: OAuthProxyProvider;
  /** Slug for the user_session_issuer we will create. */
  userSessionIssuerSlug: string;
  /** Slug for the remote_session_issuer we will create. */
  remoteSessionIssuerSlug: string;
  /**
   * Best-effort guess at the upstream issuer URL: the scheme+host of the
   * authorization_endpoint. The user can override this; many real-world
   * issuers publish a discovery document one level above the auth endpoint,
   * but the path component is unreliable. Defaults to the origin only.
   */
  issuerOriginGuess: string | null;
  /** Default session lifetime for the new user_session_issuer. */
  sessionDurationHours: number;
};

export function deriveMigrationDefaults(
  toolset: Toolset,
): MigrationDefaults | null {
  const proxy = toolset.oauthProxyServer?.oauthProxyProviders?.[0];
  if (!proxy) return null;

  return {
    proxyProvider: proxy,
    userSessionIssuerSlug: `${toolset.slug}-usi`,
    remoteSessionIssuerSlug: `${toolset.slug}-rsi`,
    issuerOriginGuess: extractOrigin(proxy.authorizationEndpoint),
    sessionDurationHours: DEFAULT_SESSION_DURATION_HOURS,
  };
}

function extractOrigin(url: string | null | undefined): string | null {
  if (!url) return null;
  try {
    const parsed = new URL(url);
    return `${parsed.protocol}//${parsed.host}`;
  } catch {
    return null;
  }
}

// canCloneProvider mirrors the server-side admin endpoint's preconditions so
// the UI can pre-empt obviously-broken submissions: gram-managed providers
// don't carry a clonable upstream client.
export function canCloneProvider(provider: OAuthProxyProvider): boolean {
  return provider.providerType === "custom";
}
