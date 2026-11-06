/** Built-in starting points; selecting one stages an editable tenant-only draft. */
export const onboardingPresets = [
  {
    id: "anthropic-enterprise",
    name: "Anthropic Enterprise Rollout",
    description:
      "Connect identity and logging, then configure Claude administration, observability, and initial security policies.",
    stepSlugs: [
      "identity-provider",
      "enable-logging",
      "anthropic-admin-controls",
      "anthropic-observability",
      "configure-policies",
    ],
  },
  {
    id: "coding-agent-observability",
    name: "Coding-Agent Observability",
    description:
      "Connect identity and logging, instrument coding agents, confirm traffic, and configure initial security policies.",
    stepSlugs: [
      "identity-provider",
      "enable-logging",
      "agent-observability",
      "configure-policies",
    ],
  },
  {
    id: "managed-mcp-distribution",
    name: "Managed MCP Distribution",
    description:
      "Connect identity and logging, configure initial security policies, then publish and distribute approved MCP servers through a marketplace.",
    stepSlugs: [
      "identity-provider",
      "enable-logging",
      "configure-policies",
      "mcp-distribution",
    ],
  },
] as const;
