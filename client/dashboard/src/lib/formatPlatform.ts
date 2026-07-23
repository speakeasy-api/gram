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
  cowork: "Claude Cowork",
  "claude-cowork": "Claude Cowork",
  cursor: "Cursor",
  codex: "Codex",
  opencode: "opencode",
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
