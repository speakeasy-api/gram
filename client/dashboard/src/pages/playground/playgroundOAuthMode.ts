import type { Toolset } from "@/lib/toolTypes";

export type OAuthMode = "none" | "custom-proxy" | "external";

/**
 * Detect the OAuth mode from toolset-level configuration.
 * - "custom-proxy": OAuth proxy server with a custom provider (user must connect)
 * - "external": External OAuth server metadata (user must connect)
 * - "none": No OAuth needed (either no OAuth config, or Gram proxy which is transparent)
 */
export function getToolsetOAuthMode(toolset: {
  oauthProxyServer?: Toolset["oauthProxyServer"];
  externalOauthServer?: Toolset["externalOauthServer"];
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
  oauthProxyServer?: Toolset["oauthProxyServer"];
  externalOauthServer?: Toolset["externalOauthServer"];
  name: string;
}): string {
  if (toolset.oauthProxyServer) {
    const provider = toolset.oauthProxyServer.oauthProxyProviders?.[0];
    if (provider) return provider.slug;
  }
  if (toolset.externalOauthServer) return toolset.externalOauthServer.slug;
  return toolset.name;
}
