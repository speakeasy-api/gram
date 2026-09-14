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
const DEDICATED_CARD_SOURCES = [isAnthropicOrCursorSource, isLiteLLMSource];

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
