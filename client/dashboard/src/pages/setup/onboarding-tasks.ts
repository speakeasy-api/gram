import type { SetupTask } from "@gram/client/models/components/setuptask.js";

import { SETUP_CARDS, setupCard } from "./setup-cards";

// Task content, aliases, and owner hints share the central card registry.
export type TaskStatus = SetupTask["status"];

const IT_ADMIN = "IT Admin";
const ENGINEERING_LEAD = "Engineering Lead";
const SECURITY_LEAD = "Security Lead";
export const ONBOARDING_TASKS = SETUP_CARDS;
export const ONBOARDING_TASK_IDS = Object.keys(SETUP_CARDS);
export function isOnboardingTaskId(value: string): boolean {
  return setupCard(value) !== undefined;
}
export function suggestedOwnerFor(key: string): string {
  return setupCard(key)?.suggestedOwner ?? "Unassigned";
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
