/**
 * Hook events from Anthropic products: Claude Code, Claude Desktop, and
 * Claude Cowork (which reports under either of its two source names).
 */
export function isAnthropicSource(source: string): boolean {
  return source.startsWith("claude") || source === "cowork";
}

/** Everything the other-platforms card covers: not an Anthropic source. */
export function isNotAnthropicSource(source: string): boolean {
  return !isAnthropicSource(source);
}
