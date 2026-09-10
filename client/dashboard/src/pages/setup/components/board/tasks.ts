export type TaskStatus = "todo" | "in_progress" | "awaiting_support" | "done";

/** Column order on the board. */
export const TASK_STATUSES: TaskStatus[] = [
  "todo",
  "in_progress",
  "awaiting_support",
  "done",
];

export const TASK_STATUS_META: Record<
  TaskStatus,
  { label: string; hint: string; dotClassName: string }
> = {
  todo: {
    label: "To Do",
    hint: "Not started",
    dotClassName: "bg-muted-foreground/40",
  },
  in_progress: {
    label: "In Progress",
    hint: "Being worked on",
    dotClassName: "bg-information-default",
  },
  awaiting_support: {
    label: "Awaiting Support",
    hint: "Waiting on Speakeasy",
    dotClassName: "bg-warning-default",
  },
  done: {
    label: "Done",
    hint: "Complete",
    dotClassName: "bg-success-default",
  },
};

export type OnboardingTaskId =
  | "identity-provider"
  | "anthropic-observability"
  | "anthropic-admin-controls"
  | "connect-idp"
  | "directory-sync"
  | "create-marketplace"
  | "instrument-agents"
  | "additional-agent-config"
  | "confirm-traffic"
  | "distribute-servers"
  | "configure-policies"
  | "platform-mcp";

export interface OnboardingTaskDefinition {
  id: OnboardingTaskId;
  title: string;
  description: string;
  /**
   * The role in the customer's organization that usually owns this task.
   * Shown as the card's eyebrow so the board reads as a checklist of
   * responsibilities to hand out, not just a list of steps.
   */
  suggestedOwner: string;
  /** Optional inline marker after the title, e.g. "Optional". */
  badge?: string;
}

const IT_ADMIN = "IT admin";
const ENGINEERING_LEAD = "Engineering lead";
const SECURITY_LEAD = "Security lead";

export const ONBOARDING_TASKS: OnboardingTaskDefinition[] = [
  {
    id: "identity-provider",
    title: "Set up identity provider",
    description: "Connect SSO and sync people and groups",
    suggestedOwner: IT_ADMIN,
  },
  {
    id: "anthropic-observability",
    title: "Set up Anthropic observability",
    description:
      "Enable inference hooks and confirm Claude conversations arrive",
    suggestedOwner: ENGINEERING_LEAD,
  },
  {
    id: "anthropic-admin-controls",
    title: "Set up Anthropic admin controls",
    description: "Publish the marketplace and connect Claude Code and Cowork",
    suggestedOwner: IT_ADMIN,
  },
  {
    id: "connect-idp",
    title: "Connect identity provider",
    description: "Link SSO for authentication",
    suggestedOwner: IT_ADMIN,
  },
  {
    id: "directory-sync",
    title: "Directory sync",
    description: "Confirm users and roles",
    suggestedOwner: IT_ADMIN,
  },
  {
    id: "create-marketplace",
    title: "Create plugin marketplace",
    description: "For distributing servers to your users",
    suggestedOwner: ENGINEERING_LEAD,
  },
  {
    id: "instrument-agents",
    title: "Set up observability in other platforms",
    description: "Connect other AI coding assistants",
    suggestedOwner: ENGINEERING_LEAD,
  },
  {
    id: "additional-agent-config",
    title: "Additional agent configuration",
    description: "Optional API keys for usage and compliance data",
    suggestedOwner: ENGINEERING_LEAD,
  },
  {
    id: "confirm-traffic",
    title: "Confirm traffic",
    description: "Verify connectivity and compliance",
    suggestedOwner: SECURITY_LEAD,
  },
  {
    id: "distribute-servers",
    title: "Distribute MCP servers",
    description: "Choose some MCP Servers to distribute to your organization",
    suggestedOwner: ENGINEERING_LEAD,
  },
  {
    id: "configure-policies",
    title: "Configure policies",
    description: "Pick the categories to flag in agent traffic",
    suggestedOwner: SECURITY_LEAD,
  },
  {
    id: "platform-mcp",
    title: "Set up Platform MCP",
    description: "Optional agent-assisted MCP setup",
    suggestedOwner: ENGINEERING_LEAD,
    badge: "Optional",
  },
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
