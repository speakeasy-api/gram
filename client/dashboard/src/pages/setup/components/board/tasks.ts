export type OnboardingTaskId =
  | "identity-provider"
  | "anthropic-observability"
  | "anthropic-admin-controls"
  | "connect-idp"
  | "directory-sync"
  | "enable-logging"
  | "create-marketplace"
  | "instrument-agents"
  | "additional-agent-config"
  | "confirm-traffic"
  | "distribute-servers"
  | "configure-policies"
  | "platform-mcp";

export interface OnboardingTaskDefinition {
  id: OnboardingTaskId;
}

export const ONBOARDING_TASKS: OnboardingTaskDefinition[] = [
  { id: "identity-provider" },
  { id: "connect-idp" },
  { id: "directory-sync" },
  { id: "enable-logging" },
  { id: "anthropic-observability" },
  { id: "anthropic-admin-controls" },
  { id: "create-marketplace" },
  { id: "instrument-agents" },
  { id: "additional-agent-config" },
  { id: "confirm-traffic" },
  { id: "distribute-servers" },
  { id: "configure-policies" },
  { id: "platform-mcp" },
];

export interface OnboardingWorkstreamDefinition {
  id: "connect" | "observe" | "distribute" | "secure";
  title: string;
  description: string;
  taskIds: OnboardingTaskId[];
}

/** Outcome-oriented groups for the consolidated onboarding tasks. */
export const ONBOARDING_WORKSTREAMS: OnboardingWorkstreamDefinition[] = [
  {
    id: "connect",
    title: "Connect identity",
    description: "Authenticate people and agents. Sync IDP roles.",
    taskIds: ["identity-provider", "connect-idp", "directory-sync"],
  },
  {
    id: "observe",
    title: "Observe agents",
    description: "Instrument agents, add integrations, and verify traffic.",
    taskIds: [
      "enable-logging",
      "anthropic-observability",
      "instrument-agents",
      "additional-agent-config",
      "confirm-traffic",
    ],
  },
  {
    id: "distribute",
    title: "MCP Gateway",
    description: "Publish and distribute approved MCP servers.",
    taskIds: ["create-marketplace", "distribute-servers", "platform-mcp"],
  },
  {
    id: "secure",
    title: "Secure agent traffic",
    description: "Apply the initial policy controls.",
    taskIds: ["anthropic-admin-controls", "configure-policies"],
  },
];

export function isOnboardingTaskId(value: string): value is OnboardingTaskId {
  return ONBOARDING_TASKS.some((task) => task.id === value);
}
