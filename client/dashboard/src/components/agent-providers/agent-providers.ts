export const AGENT_PROVIDERS = {
  claude: {
    name: "Claude Code",
    description: "Anthropic CLI & IDE agent",
    iconSource: "claude-code",
  },
  "claude-cowork": {
    name: "Claude Cowork",
    description: "Autonomous AI desktop assistant",
    iconSource: "claude-cowork",
  },
  cursor: {
    name: "Cursor",
    description: "AI-powered code editor",
    iconSource: "cursor",
  },
  codex: {
    name: "OpenAI Codex",
    description: "Codex CLI & Codex mode in the ChatGPT app",
    iconSource: "codex",
  },
  litellm: {
    name: "LiteLLM",
    description: "Open-source LLM gateway and proxy",
    iconSource: "litellm",
  },
  opencode: {
    name: "opencode",
    description: "Open-source terminal coding agent",
    iconSource: "opencode",
  },
  openclaw: {
    name: "OpenClaw",
    description: "Open-source personal AI agent gateway",
    iconSource: "openclaw",
  },
  copilot: {
    name: "GitHub Copilot",
    description: "Microsoft / GitHub AI pair programmer",
    iconSource: "copilot",
  },
  gemini: {
    name: "Gemini",
    description: "Google's coding assistant",
    iconSource: "gemini",
  },
  glean: {
    name: "Glean",
    description: "Enterprise work assistant",
    iconSource: "glean",
  },
  bedrock: {
    name: "AWS Bedrock",
    description: "Amazon's managed foundation model service",
    iconSource: "aws-bedrock",
  },
  devin: {
    name: "Devin",
    description: "Cognition's autonomous software engineer",
    iconSource: "devin",
  },
  mistral: {
    name: "Mistral",
    description: "Mistral AI coding assistant",
    iconSource: "mistral",
  },
} as const;

export type AgentProviderId = keyof typeof AGENT_PROVIDERS;

export type AgentProvider = {
  id: AgentProviderId;
  name: string;
  description: string;
  iconSource: string;
};

export type AgentProviderSurface = keyof typeof ACTIVE_AGENT_PROVIDER_IDS;

export const COMING_SOON_AGENT_PROVIDER_IDS = [
  "copilot",
  "gemini",
  "glean",
  "bedrock",
  "devin",
  "mistral",
] as const satisfies readonly AgentProviderId[];

export const ACTIVE_AGENT_PROVIDER_IDS = {
  hooks: ["claude", "cursor", "codex"],
  plugins: [
    "claude",
    "claude-cowork",
    "cursor",
    "codex",
    "opencode",
    "openclaw",
  ],
  setup: ["claude", "claude-cowork", "codex", "cursor", "opencode", "openclaw"],
} as const satisfies Record<string, readonly AgentProviderId[]>;

export function agentProvidersForSurface(
  surface: AgentProviderSurface,
): Array<AgentProvider & { available: boolean }> {
  return [
    ...ACTIVE_AGENT_PROVIDER_IDS[surface].map((id) => ({
      id,
      ...AGENT_PROVIDERS[id],
      available: true,
    })),
    ...COMING_SOON_AGENT_PROVIDER_IDS.map((id) => ({
      id,
      ...AGENT_PROVIDERS[id],
      available: false,
    })),
  ];
}
