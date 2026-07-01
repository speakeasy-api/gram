/**
 * Format a raw agent/client "source" string (e.g. "claude-code", "aws-bedrock")
 * into a human-readable label ("Claude Code", "Aws Bedrock") by splitting on
 * `-`/`_` and title-casing each part.
 *
 * Shared by the observability pages so source labels are consistent everywhere
 * and derived from the data rather than a hardcoded per-page catalog.
 */
export function formatPlatform(value: string): string {
  return value
    .split(/[-_]/)
    .filter(Boolean)
    .map((part) => part[0]!.toUpperCase() + part.slice(1))
    .join(" ");
}
