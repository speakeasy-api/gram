export type OAuthMode = "none" | "custom-proxy" | "external";

/** Minimal shape of the proxy server fields we inspect. */
type OAuthProxyServerLike = {
  oauthProxyProviders?: Array<{ providerType: string; slug: string }>;
};

/** Minimal shape of the external OAuth server fields we inspect. */
type ExternalOAuthServerLike = {
  slug: string;
};

/**
 * Detect the OAuth mode from toolset-level configuration.
 * - "custom-proxy": OAuth proxy server with a custom provider (user must connect)
 * - "external": External OAuth server metadata (user must connect)
 * - "none": No OAuth needed (either no OAuth config, or Gram proxy which is transparent)
 */
export function getToolsetOAuthMode(toolset: {
  oauthProxyServer?: OAuthProxyServerLike;
  externalOauthServer?: ExternalOAuthServerLike;
}): OAuthMode {
  if (toolset.oauthProxyServer) {
    const provider = toolset.oauthProxyServer.oauthProxyProviders?.[0];
    if (provider?.providerType === "gram") return "none"; // transparent via Gram session
    if (provider?.providerType === "custom") return "custom-proxy";
  }
  if (toolset.externalOauthServer) return "external";
  return "none";
}

/**
 * Derive a human-readable provider name from toolset OAuth config.
 */
export function getOAuthProviderName(toolset: {
  oauthProxyServer?: OAuthProxyServerLike;
  externalOauthServer?: ExternalOAuthServerLike;
  name: string;
}): string {
  if (toolset.oauthProxyServer) {
    const provider = toolset.oauthProxyServer.oauthProxyProviders?.[0];
    if (provider) return provider.slug;
  }
  if (toolset.externalOauthServer) return toolset.externalOauthServer.slug;
  return toolset.name;
}
