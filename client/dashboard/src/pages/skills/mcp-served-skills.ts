/**
 * The digest an agent sees for a skill version over MCP: the SHA-256 of the
 * exact SKILL.md bytes, in the `sha256:<hex>` form the Skills extension uses.
 */
export function servedSkillDigest(rawSha256: string): string {
  return `sha256:${rawSha256.toLowerCase()}`;
}

/** A readable prefix of the digest for labels; the full value goes in a tooltip. */
export function shortServedSkillDigest(rawSha256: string): string {
  return `sha256:${rawSha256.slice(0, 12).toLowerCase()}…`;
}
