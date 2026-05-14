import type { Toolset } from "@/lib/toolTypes";
import type { OAuthProxyProvider } from "@gram/client/models/components";

// Pure helpers that derive migration defaults from a toolset's first
// oauth_proxy_provider. Kept separate from React so the logic stays unit
// testable without rendering hooks.

// Default to 2 weeks — middle of the three options the modal offers
// (1 week / 2 weeks / 1 month). Keep in lockstep with
// REFRESH_TOKEN_DURATION_OPTIONS in WireUserSessionIssuerModal so the
// selector opens on a real option rather than a custom value.
export const DEFAULT_SESSION_DURATION_HOURS = 24 * 14;

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

  // Slug is `{toolset.slug}-{random}`. The toolset name is what an operator
  // recognises in a list; the resource type is not — the link is the FK on
  // toolsets.user_session_issuer_id, not the slug. The user session issuer
  // and the remote session issuer live in different tables with independent
  // uniqueness, so the same value works for both and reads as a pair. 4
  // bytes of entropy is plenty for project-scoped uniqueness.
  const slug = `${toolset.slug}-${randomSlugSuffix()}`;
  return {
    proxyProvider: proxy,
    userSessionIssuerSlug: slug,
    remoteSessionIssuerSlug: slug,
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

function randomSlugSuffix(): string {
  const bytes = new Uint8Array(4);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}
