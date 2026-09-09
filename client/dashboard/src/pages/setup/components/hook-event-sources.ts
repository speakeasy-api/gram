/**
 * Hook events from Anthropic products: Claude Code, Claude Chat (desktop and
 * web), and Claude Cowork (which reports under either of its two source names).
 */
function isAnthropicSource(source: string): boolean {
  return source.startsWith("claude") || source === "cowork";
}

/**
 * What the Anthropic observability card sets up: the Claude products plus,
 * optionally, Cursor, which imports the plugin from the same marketplace.
 */
export function isAnthropicOrCursorSource(source: string): boolean {
  return isAnthropicSource(source) || source === "cursor";
}

/** Everything the device agent covers on the other-platforms card. */
export function isOtherPlatformSource(source: string): boolean {
  return !isAnthropicOrCursorSource(source);
}
