// Product surfaces are distinct from their providers: for example, Anthropic is
// a provider while Claude Chat Desktop, Claude Code, and Claude Cowork are
// separate surfaces. Raw source values come from several generations of hook
// and compliance ingestion, so aliases must share one label across reporting.
const PRODUCT_SURFACE_LABELS: Record<string, string> = {
  claude: "Claude Chat Desktop",
  "claude-desktop": "Claude Chat Desktop",
  "claude-chat-desktop": "Claude Chat Desktop",
  "claude-web": "Claude Chat Web",
  "claude-chat-web": "Claude Chat Web",
  claudecode: "Claude Code",
  "claude-code": "Claude Code",
  // Claude Code Desktop is its own surface, distinct from the claude-code CLI
  // and from cowork (which shares CCD's hook adapter slug but resolves to
  // "cowork" server-side via the OTEL service.name).
  "claude-code-desktop": "Claude Code Desktop",
  cowork: "Claude Cowork",
  "claude-cowork": "Claude Cowork",
  cursor: "Cursor",
  codex: "Codex",
  "codex-web": "Codex Web",
  // ChatGPT (web/desktop chat) and ChatGPT Work, observed via the OpenAI
  // compliance import — distinct surfaces from the Codex agent, split at
  // ingest by hook_source so they stay separable in the summaries.
  chatgpt: "ChatGPT",
  "chatgpt-work": "ChatGPT Work",
  opencode: "opencode",
  litellm: "LiteLLM",
  copilot: "Copilot",
  "github-copilot": "Copilot",
  gemini: "Gemini",
  glean: "Glean",
  bedrock: "AWS Bedrock",
  "aws-bedrock": "AWS Bedrock",
};

/**
 * Format a raw agent/client source as its normalized product-surface label.
 * Unknown sources fall back to delimiter-aware title casing.
 */
export function formatPlatform(value: string): string {
  const trimmed = value.trim();
  const normalized = trimmed.toLowerCase().replace(/[\s_]+/g, "-");
  const known = PRODUCT_SURFACE_LABELS[normalized];
  if (known) return known;

  return trimmed
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((part) => part[0]!.toUpperCase() + part.slice(1))
    .join(" ");
}

/**
 * Format a chat's full source label, including its LiteLLM proxy relationship.
 * Proxy-owned transcripts name the client behind the proxy ("Claude Code via
 * LiteLLM"); natively captured sessions the proxy also observed name the
 * surface with the proxy appended ("Claude Code via LiteLLM" with
 * source=claude-code) — proxied rows are suppressed for those sessions, so the
 * source alone would hide the LiteLLM association.
 */
export function formatChatSource(
  source: string,
  chat: { originatingClient?: string; litellmProxied?: boolean },
): string {
  if (chat.originatingClient) {
    return `${formatPlatform(chat.originatingClient)} via ${formatPlatform(source)}`;
  }
  if (chat.litellmProxied && source !== "litellm") {
    return `${formatPlatform(source)} via ${formatPlatform("litellm")}`;
  }
  return formatPlatform(source);
}
