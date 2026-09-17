// Kept apart from AIToolIcon.tsx so that file exports only components: the
// fast-refresh lint refuses a shared constant next to one.

/**
 * Vendor marks by aivendors registry id.
 *
 * Mapped explicitly rather than through agentProviderIconKind, whose substring
 * matching would give an organization's own "cursor-fork" target the Cursor
 * logo. In the table people read to decide what to block, a confident wrong
 * mark is worse than none, so anything unlisted gets the monogram.
 */
export const ICON_SOURCE_BY_TARGET_ID: Record<string, string> = {
  "claude-code": "claude",
  claude: "claude",
  // ChatGPT and Codex share OpenAI's mark; AgentProviderIcon resolves both
  // through its codex case, which is the OpenAI monoblossom.
  chatgpt: "chatgpt",
  "chatgpt-classic": "chatgpt",
  codex: "codex",
  cursor: "cursor",
  opencode: "opencode",
  openclaw: "openclaw",
  "gemini-cli": "gemini",
  windsurf: "windsurf",
  ollama: "ollama",
  lmstudio: "lmstudio",
  "hermes-agent": "hermes",
  cline: "cline",
  warp: "warp",
  trae: "trae",
  vllm: "vllm",
  // Devin and Windsurf are one vendor's two shapes: Devin Desktop is the
  // rebranded Windsurf, so both carry Cognition's mark.
  devin: "devin",
};

// Every target id AIToolIcon has a mark for, so the test can walk the map
// instead of trusting a hand-kept list to stay in step with it.
export const ICON_TARGET_IDS: readonly string[] = Object.keys(
  ICON_SOURCE_BY_TARGET_ID,
);
