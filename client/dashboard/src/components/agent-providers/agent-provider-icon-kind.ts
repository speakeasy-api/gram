export type AgentProviderIconKind =
  | "claude"
  | "cursor"
  | "codex"
  | "opencode"
  | "openclaw"
  | "pi"
  | "litellm"
  | "cline"
  | "warp"
  | "trae"
  | "vllm"
  | "ollama"
  | "lmstudio"
  | "windsurf"
  | "hermes"
  | "devin"
  | "mistral"
  | "copilot"
  | "gemini"
  | "glean"
  | "bedrock"
  | "catchall"
  | "unknown";

/**
 * Whether a source resolves to a real provider mark rather than the generic
 * fallback. Callers that would rather show nothing than a globe use this.
 */
export function hasAgentProviderIcon(source?: string): boolean {
  const kind = agentProviderIconKind(source);
  return kind !== "unknown" && kind !== "catchall";
}

export function agentProviderIconKind(source?: string): AgentProviderIconKind {
  const normalizedSource = source
    ?.trim()
    .toLowerCase()
    .replace(/[\s_]+/g, "-");

  if (
    normalizedSource === "anthropic" ||
    normalizedSource?.includes("claude") ||
    normalizedSource?.includes("cowork")
  ) {
    return "claude";
  }
  if (normalizedSource?.includes("cursor")) return "cursor";
  if (
    normalizedSource === "openai" ||
    normalizedSource?.includes("codex") ||
    normalizedSource?.includes("chatgpt")
  ) {
    return "codex";
  }
  if (normalizedSource?.includes("opencode")) return "opencode";
  if (normalizedSource?.includes("openclaw")) return "openclaw";
  // Matched exactly, not by substring: "pi" is a substring of other agent
  // names ("copilot"), and Pi's hook source is always the bare slug.
  if (normalizedSource === "pi") return "pi";
  if (normalizedSource?.includes("litellm")) return "litellm";
  if (normalizedSource?.includes("cline")) return "cline";
  if (normalizedSource?.includes("warp")) return "warp";
  if (normalizedSource?.includes("trae")) return "trae";
  if (normalizedSource?.includes("vllm")) return "vllm";
  if (normalizedSource?.includes("ollama")) return "ollama";
  // Matches both the "lmstudio" target id and the "lm-studio" the normalizer
  // produces from "LM Studio".
  if (
    normalizedSource?.includes("lmstudio") ||
    normalizedSource?.includes("lm-studio")
  ) {
    return "lmstudio";
  }
  if (
    normalizedSource?.includes("windsurf") ||
    normalizedSource === "codeium"
  ) {
    return "windsurf";
  }
  if (
    normalizedSource?.includes("hermes") ||
    normalizedSource === "nousresearch"
  ) {
    return "hermes";
  }
  if (normalizedSource?.includes("devin")) return "devin";
  if (normalizedSource?.includes("mistral")) return "mistral";
  if (
    normalizedSource?.includes("copilot") ||
    normalizedSource?.includes("microsoft")
  ) {
    return "copilot";
  }
  if (normalizedSource === "google" || normalizedSource?.includes("gemini")) {
    return "gemini";
  }
  if (normalizedSource?.includes("glean")) return "glean";
  if (
    normalizedSource?.includes("bedrock") ||
    normalizedSource?.includes("aws")
  ) {
    return "bedrock";
  }
  // The catch-all agent has no vendor mark to carry, so the globe is its
  // deliberate icon rather than the fallback an unrecognised source lands on.
  if (normalizedSource === "other") return "catchall";

  return "unknown";
}
