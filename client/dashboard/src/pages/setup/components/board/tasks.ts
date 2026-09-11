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
  { id: "identity-provider", suggestedOwner: IT_ADMIN },
  { id: "connect-idp", suggestedOwner: IT_ADMIN },
  { id: "directory-sync", suggestedOwner: IT_ADMIN },
  { id: "enable-logging", suggestedOwner: ENGINEERING_LEAD },
  { id: "anthropic-observability", suggestedOwner: ENGINEERING_LEAD },
  { id: "anthropic-admin-controls", suggestedOwner: IT_ADMIN },
  { id: "create-marketplace", suggestedOwner: ENGINEERING_LEAD },
  { id: "instrument-agents", suggestedOwner: ENGINEERING_LEAD },
  { id: "additional-agent-config", suggestedOwner: ENGINEERING_LEAD },
  { id: "confirm-traffic", suggestedOwner: SECURITY_LEAD },
  { id: "distribute-servers", suggestedOwner: ENGINEERING_LEAD },
  { id: "configure-policies", suggestedOwner: SECURITY_LEAD },
  { id: "platform-mcp", suggestedOwner: ENGINEERING_LEAD, badge: "Optional" },
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
