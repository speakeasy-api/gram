// These catalog servers use dark logos that need inversion on dark surfaces.
const DARK_MODE_INVERTED_LOGOS = new Set([
  "app.linear/linear",
  "com.pulsemcp.mirror/gram-ordinal-api",
  "com.pulsemcp.mirror/intercom",
  "com.pulsemcp.mirror/launchdarkly-mcp-server",
  "com.pulsemcp.mirror/mercury",
  "com.pulsemcp.mirror/xdevplatform-xmcp",
  "com.vercel/vercel-mcp",
  "io.github.github/github-mcp-server",
]);

export function catalogLogoClassName(registrySpecifier: string): string {
  return DARK_MODE_INVERTED_LOGOS.has(registrySpecifier) ? "dark:invert" : "";
}
