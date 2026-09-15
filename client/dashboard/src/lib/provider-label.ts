/**
 * Hostnames a provider slug commonly carries that say nothing about which
 * provider it is. Stripped so a row reads "Linear" rather than
 * "mcp.linear.app".
 */
const NOISE_LABELS = new Set(["mcp", "oauth", "auth", "api", "www", "app"]);

/**
 * Human name for an upstream provider, derived from its slug.
 *
 * Slugs are hostnames (`mcp.linear.app`, `oauth.googleapis.com`), which are
 * exact but read as infrastructure. The full slug stays available for the
 * tooltip and the expanded detail, where precision matters more than scanning.
 *
 * Deliberately a display-only transform: two providers could in principle
 * shorten to the same word, so nothing keys off the result.
 */
export function providerLabel(slug: string): string {
  const host = slug.replace(/^https?:\/\//, "").split("/")[0] ?? slug;
  const parts = host.split(".").filter((part) => part.length > 0);

  // Drop the TLD and any leading service prefix, leaving the registrable name.
  const meaningful = parts
    .slice(0, Math.max(parts.length - 1, 1))
    .filter((part) => !NOISE_LABELS.has(part.toLowerCase()));

  const name = meaningful.at(-1) ?? host;
  // "googleapis" and friends read better without the api suffix.
  const trimmed = name.replace(/apis?$/i, "") || name;

  return trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
}
