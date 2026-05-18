export type AgentProviderId =
  | "claude-code"
  | "claude-cowork"
  | "cursor"
  | "codex"
  | "copilot"
  | "gemini"
  | "glean"
  | "bedrock";

export type AgentProvider = {
  id: AgentProviderId;
  label: string;
  source: string;
  available: boolean;
};

export const AGENT_PROVIDERS: AgentProvider[] = [
  {
    id: "claude-code",
    label: "Claude Code",
    source: "claude-code",
    available: true,
  },
  {
    id: "claude-cowork",
    label: "Claude Cowork",
    source: "cowork",
    available: true,
  },
  { id: "cursor", label: "Cursor", source: "cursor", available: true },
  { id: "codex", label: "Codex", source: "codex", available: true },
  { id: "copilot", label: "Copilot", source: "copilot", available: false },
  { id: "gemini", label: "Gemini", source: "gemini", available: false },
  { id: "glean", label: "Glean", source: "glean", available: false },
  {
    id: "bedrock",
    label: "AWS Bedrock",
    source: "aws-bedrock",
    available: false,
  },
];
