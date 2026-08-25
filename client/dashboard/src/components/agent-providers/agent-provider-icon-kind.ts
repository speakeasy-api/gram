export type AgentProviderIconKind =
  | "claude"
  | "cursor"
  | "codex"
  | "opencode"
  | "litellm"
  | "devin"
  | "mistral"
  | "copilot"
  | "gemini"
  | "glean"
  | "bedrock"
  | "unknown";

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
  if (normalizedSource?.includes("litellm")) return "litellm";
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

  return "unknown";
}
