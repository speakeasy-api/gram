export type SurfaceId = "cc" | "chat" | "cowork" | "codex" | "cursor" | "other";
export type CapabilityId =
  | "session"
  | "blocking"
  | "identity"
  | "cost"
  | "shadow";

export const surfaces = [
  { id: "cc" as const, name: "Claude Code", detail: "CLI · Desktop · Cloud" },
  { id: "chat" as const, name: "Claude Chat", detail: "Web · Desktop" },
  { id: "cowork" as const, name: "Cowork", detail: "Desktop · Cloud · Web" },
  {
    id: "codex" as const,
    name: "Codex / ChatGPT",
    detail: "CLI · Desktop · Web",
  },
  { id: "cursor" as const, name: "Cursor", detail: "IDE · CLI · Cloud" },
  {
    id: "other" as const,
    name: "Other agents",
    detail: "OpenCode · Gemini · Copilot",
  },
];

export const capabilities = [
  {
    id: "session" as const,
    name: "Session activity observed",
    description: "Aggregate chat-session activity",
  },
  {
    id: "blocking" as const,
    name: "Policy enforcement",
    description: "Synchronous policy-decision evidence",
  },
  {
    id: "identity" as const,
    name: "Identity attribution",
    description: "Activity bound to an organization or user",
  },
  {
    id: "cost" as const,
    name: "Token usage observed",
    description: "Aggregate token usage by surface",
  },
  {
    id: "shadow" as const,
    name: "Shadow MCP & AI",
    description: "Unsanctioned clients and servers",
  },
];

export type IntegrationMethod = {
  id: string;
  name: string;
  description: string;
  setup: string;
  surfaces: SurfaceId[];
  capabilities: CapabilityId[];
};

export const methods: IntegrationMethod[] = [
  {
    id: "inference-hooks",
    name: "Anthropic Inference Hooks",
    description: "Broad Claude coverage with synchronous policy decisions.",
    setup: "About 10 minutes · Anthropic console",
    surfaces: ["cc", "chat", "cowork"],
    capabilities: ["session", "blocking"],
  },
  {
    id: "client-hooks",
    name: "Client hooks",
    description:
      "Local session, tool, and usage evidence from supported developer agents.",
    setup: "About 20 minutes · managed settings",
    surfaces: ["cc", "cowork", "codex", "cursor", "other"],
    capabilities: ["session", "blocking", "cost"],
  },
  {
    id: "device-agent",
    name: "Device Agent",
    description:
      "Device-local inventory and activity, including shadow AI and MCP discovery.",
    setup: "1–2 days · fleet rollout",
    surfaces: ["cc", "codex", "cursor", "other"],
    capabilities: ["session", "identity", "cost", "shadow"],
  },
  {
    id: "provider-import",
    name: "Provider admin integrations",
    description:
      "Read-only workspace imports for conversations, usage, and spend.",
    setup: "5–15 minutes · provider admin key",
    surfaces: ["chat", "codex", "cursor"],
    capabilities: ["session", "cost", "identity"],
  },
  {
    id: "otel",
    name: "OpenTelemetry export",
    description: "Usage and session evidence through an existing collector.",
    setup: "About 45 minutes · collector config",
    surfaces: ["cc", "cowork"],
    capabilities: ["session", "cost"],
  },
];

export function surfaceForHookSource(value: string): SurfaceId {
  const source = value.toLowerCase();
  if (source.includes("cowork")) return "cowork";
  if (source.includes("claude-code") || source === "claudecode") return "cc";
  if (
    source === "claude" ||
    source.includes("claude-chat") ||
    source.includes("claude-desktop")
  )
    return "chat";
  if (source.includes("cursor")) return "cursor";
  if (
    source.includes("codex") ||
    source.includes("openai") ||
    source.includes("chatgpt")
  )
    return "codex";
  return "other";
}
