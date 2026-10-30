// These catalog servers use dark logos that need inversion on dark surfaces.
const DARK_MODE_INVERTED_LOGOS = new Set([
  "app.linear/linear",
  "com.vercel/vercel-mcp",
  "io.github.github/github-mcp-server",
]);

export function catalogLogoClassName(registrySpecifier: string): string {
  return DARK_MODE_INVERTED_LOGOS.has(registrySpecifier) ? "dark:invert" : "";
}
