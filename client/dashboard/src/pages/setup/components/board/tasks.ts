import type { SetupWorkstream } from "@gram/client/models/components/setupworkstream.js";

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
  | "domain-verification"
  | "identity-provider"
  | "anthropic-observability"
  | "anthropic-admin-controls"
  | "connect-idp"
  | "directory-sync"
  | "enable-logging"
  | "create-marketplace"
  | "litellm"
  | "instrument-agents"
  | "additional-agent-config"
  | "confirm-traffic"
  | "distribute-servers"
  | "configure-policies"
  | "platform-mcp";

export interface OnboardingTaskDefinition {
  id: OnboardingTaskId;
  suggestedOwner: string;
  badge?: string;
}

const IT_ADMIN = "IT Admin";
const ENGINEERING_LEAD = "Engineering Lead";
const SECURITY_LEAD = "Security Lead";

export const ONBOARDING_TASKS: OnboardingTaskDefinition[] = [
  { id: "domain-verification", suggestedOwner: IT_ADMIN },
  { id: "identity-provider", suggestedOwner: IT_ADMIN },
  { id: "connect-idp", suggestedOwner: IT_ADMIN },
  { id: "directory-sync", suggestedOwner: IT_ADMIN },
  { id: "enable-logging", suggestedOwner: ENGINEERING_LEAD },
  { id: "anthropic-observability", suggestedOwner: ENGINEERING_LEAD },
  { id: "anthropic-admin-controls", suggestedOwner: IT_ADMIN },
  { id: "create-marketplace", suggestedOwner: ENGINEERING_LEAD },
  { id: "instrument-agents", suggestedOwner: ENGINEERING_LEAD },
  { id: "litellm", suggestedOwner: ENGINEERING_LEAD },
  { id: "additional-agent-config", suggestedOwner: ENGINEERING_LEAD },
  { id: "confirm-traffic", suggestedOwner: SECURITY_LEAD },
  { id: "distribute-servers", suggestedOwner: ENGINEERING_LEAD },
  { id: "configure-policies", suggestedOwner: SECURITY_LEAD },
  { id: "platform-mcp", suggestedOwner: ENGINEERING_LEAD, badge: "Optional" },
];

// Only presentation hints live here. Titles, membership, and ordering come from the API.
export type OnboardingWorkstreamDefinition = SetupWorkstream & {
  description: string;
  suggestedOwner: string;
};

const WORKSTREAM_PRESENTATION: Record<
  string,
  { description: string; suggestedOwner: string }
> = {
  connect: {
    suggestedOwner: IT_ADMIN,
    description: "Authenticate people and agents. Sync IDP roles.",
  },
  observe: {
    suggestedOwner: ENGINEERING_LEAD,
    description: "Instrument agents, add integrations, and verify traffic.",
  },
  distribute: {
    suggestedOwner: ENGINEERING_LEAD,
    description: "Publish and distribute approved MCP servers.",
  },
  secure: {
    suggestedOwner: SECURITY_LEAD,
    description: "Apply the initial policy controls.",
  },
};

export function resolveWorkstreams(
  workstreams: SetupWorkstream[],
): OnboardingWorkstreamDefinition[] {
  return workstreams.map((workstream) => ({
    ...workstream,
    description: WORKSTREAM_PRESENTATION[workstream.id]?.description ?? "",
    suggestedOwner:
      WORKSTREAM_PRESENTATION[workstream.id]?.suggestedOwner ??
      "Assign workstream",
  }));
}

export function isOnboardingTaskId(value: string): value is OnboardingTaskId {
  return ONBOARDING_TASKS.some((task) => task.id === value);
}
