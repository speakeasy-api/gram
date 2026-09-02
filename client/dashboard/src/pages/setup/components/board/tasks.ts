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
    title: "Instrument agents",
    description: "Connect AI coding assistants",
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

export function isOnboardingTaskId(value: string): value is OnboardingTaskId {
  return ONBOARDING_TASKS.some((task) => task.id === value);
}
