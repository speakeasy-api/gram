/**
 * Hook events from Anthropic products: Claude Code, Claude Chat (desktop and
 * web), and Claude Cowork (which reports under either of its two source names).
 * What the Anthropic admin controls card sets up.
 */
export function isAnthropicSource(source: string): boolean {
  return source.startsWith("claude") || source === "cowork";
}

/**
 * Hook events from Cursor. The Cursor admin controls card confirms these, and
 * so does the other-platforms card: the device agent installs the same plugin
 * into Cursor, so Cursor is not a dedicated source.
 */
export function isCursorSource(source: string): boolean {
  return source === "cursor";
}

/**
 * Events the LiteLLM guardrail delivers. A proxy is not a machine the device
 * agent enrolls, so it has a card of its own, which confirms traffic from the
 * instance's diagnostics rather than from these events.
 */
function isLiteLLMSource(source: string): boolean {
  return source === "litellm";
}

// Every source a dedicated card owns. The other-platforms card is "everything
// else", so a new dedicated card registers here and is excluded there by
// construction rather than by another hand-written clause.
const DEDICATED_CARD_SOURCES = [isAnthropicSource, isLiteLLMSource];

/**
 * Everything the device agent covers on the other-platforms card. An event
 * with no source at all belongs to neither card: it names no platform, so
 * counting it as "everything else" would confirm traffic nobody set up.
 */
export function isOtherPlatformSource(source: string): boolean {
  return (
    source !== "" && !DEDICATED_CARD_SOURCES.some((matches) => matches(source))
  );
}
