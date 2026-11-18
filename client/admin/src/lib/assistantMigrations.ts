import type { AdminOrganizationAssistant } from "@/lib/gramAdminApi";

export type AssistantMigrationAction = "names" | "identity" | "kit";

export const MIGRATION_ACTIONS: Record<
  AssistantMigrationAction,
  {
    label: string;
    description: string;
  }
> = {
  names: {
    label: "Repair names",
    description:
      "Replace exact historical Assistant <UUID> agent names with assistant names. Custom names remain unchanged. Conflicting names are skipped and reported.",
  },
  identity: {
    label: "Migrate identities",
    description:
      "Give assistants without an agent a dedicated identity owned by their creator. They start with project-wide MCP and skill access, administration of their assistant, and inference only when their creator can delegate it. Triggers get workload identities, and turns run as the agent rather than the owner. Creators must still be active members. Existing identities are not replaced.",
  },
  kit: {
    label: "Move to Kit",
    description:
      "Switch eligible assistants to the Kit runtime. A usable agent identity, a shared user-session issuer, and an idle assistant are required. Busy or ineligible assistants are skipped and reported. This does not migrate identities or bypass safety checks.",
  },
};

export function migrationCandidates(
  assistants: AdminOrganizationAssistant[],
  action: AssistantMigrationAction,
): AdminOrganizationAssistant[] {
  return assistants.filter((assistant) => {
    switch (action) {
      case "names":
        return assistant.agent_name_repair_state === "pending";
      case "identity":
        return (
          assistant.identity_state === "NEVER_CONFIGURED" &&
          assistant.creator_active
        );
      case "kit":
        return (
          assistant.runtime_version === 2 &&
          assistant.identity_state === "ACTIVE"
        );
    }
  });
}

export function migrationCounts(assistants: AdminOrganizationAssistant[]): {
  names: number;
  identity: number;
  kit: number;
  conflicts: number;
  needsOwner: number;
  unavailable: number;
} {
  return {
    names: migrationCandidates(assistants, "names").length,
    identity: migrationCandidates(assistants, "identity").length,
    kit: migrationCandidates(assistants, "kit").length,
    conflicts: assistants.filter(
      (a) => a.agent_name_repair_state === "conflict",
    ).length,
    needsOwner: assistants.filter(
      (a) => a.identity_state === "NEVER_CONFIGURED" && !a.creator_active,
    ).length,
    unavailable: assistants.filter((a) => a.identity_state === "UNAVAILABLE")
      .length,
  };
}
