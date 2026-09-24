import type { SetupTask } from "@gram/client/models/components/setuptask.js";

// The dashboard's registry of onboarding tasks it knows how to render. Titles,
// descriptions, completion rules, progress membership, authorization and
// workstream membership are server-owned; only presentation hints and legacy
// URL aliases live here. Renderers are keyed by the same ids in
// components/board/task-step.tsx, kept apart so the board does not load every
// step component.
//
// Adding an onboarding task:
//   1. server/internal/organizations/setup_tasks.go — add the catalog entry
//      (title, description, prerequisites, HiddenByDefault, Optional) and any
//      completion fact in projectSetupTasks.
//   2. server/internal/organizations/setup_workstreams.go — add the key to
//      exactly one workstream; its position there is its display order.
//   3. Here — add the key with its suggested owner (and a slug only for a
//      pre-existing short link).
//   4. components/board/task-step.tsx — add its renderer; the map is typed
//      over this registry, so a missing one fails type-check.
// The catalog tests and task-step.test.tsx fail if the server and this
// registry disagree. A server task shipped before its renderer still shows
// on the board and in the wizard, as an explicit "needs a newer dashboard"
// card, and still counts toward progress.

export type TaskStatus = SetupTask["status"];

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

const IT_ADMIN = "IT Admin";
const ENGINEERING_LEAD = "Engineering Lead";
const SECURITY_LEAD = "Security Lead";

export interface OnboardingTaskPresentation {
  suggestedOwner: string;
  /** Short ?task= alias minted by older links; the key itself always works. */
  slug?: string;
}

export const ONBOARDING_TASKS = {
  "domain-verification": { suggestedOwner: IT_ADMIN, slug: "domain" },
  "identity-provider": { suggestedOwner: IT_ADMIN, slug: "idp" },
  "connect-idp": { suggestedOwner: IT_ADMIN },
  "directory-sync": { suggestedOwner: IT_ADMIN },
  "enable-logging": { suggestedOwner: ENGINEERING_LEAD },
  "anthropic-observability": { suggestedOwner: ENGINEERING_LEAD },
  "anthropic-admin-controls": { suggestedOwner: IT_ADMIN },
  "create-marketplace": { suggestedOwner: ENGINEERING_LEAD },
  "instrument-agents": {
    suggestedOwner: ENGINEERING_LEAD,
    slug: "other-platforms",
  },
  litellm: { suggestedOwner: ENGINEERING_LEAD },
  "additional-agent-config": {
    suggestedOwner: ENGINEERING_LEAD,
    slug: "integrations",
  },
  "confirm-traffic": { suggestedOwner: SECURITY_LEAD },
  "distribute-servers": { suggestedOwner: ENGINEERING_LEAD },
  "configure-policies": { suggestedOwner: SECURITY_LEAD, slug: "policies" },
  "platform-mcp": { suggestedOwner: ENGINEERING_LEAD },
} satisfies Record<string, OnboardingTaskPresentation>;

export type OnboardingTaskId = keyof typeof ONBOARDING_TASKS;

export const ONBOARDING_TASK_IDS = Object.keys(
  ONBOARDING_TASKS,
) as OnboardingTaskId[];

export function isOnboardingTaskId(value: string): value is OnboardingTaskId {
  return Object.hasOwn(ONBOARDING_TASKS, value);
}

/** Fallback owner hint for tasks this dashboard version does not know. */
const UNKNOWN_TASK_OWNER = "Unassigned";

export function suggestedOwnerFor(key: string): string {
  return isOnboardingTaskId(key)
    ? ONBOARDING_TASKS[key].suggestedOwner
    : UNKNOWN_TASK_OWNER;
}

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

export function workstreamPresentation(id: string): {
  description: string;
  suggestedOwner: string;
} {
  return (
    WORKSTREAM_PRESENTATION[id] ?? {
      description: "",
      suggestedOwner: "Assign workstream",
    }
  );
}

/** Humanized key for tasks whose server title is not in the response. */
export function fallbackTaskTitle(key: string): string {
  const words = key.replace(/[-_]+/g, " ").trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}
