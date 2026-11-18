import { describe, expect, it } from "vitest";
import type { AdminOrganizationAssistant } from "./gramAdminApi";
import { migrationCandidates, migrationCounts } from "./assistantMigrations";

function assistant(
  overrides: Partial<AdminOrganizationAssistant> = {},
): AdminOrganizationAssistant {
  return {
    id: "assistant-fixture",
    name: "Fixture assistant",
    project_id: "project-fixture",
    project_slug: "fixture",
    project_name: "Fixture",
    status: "active",
    identity_state: "ACTIVE",
    runtime_version: 2,
    creator_active: true,
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("assistant migration candidates", () => {
  it("repairs only explicit pending names, preserving custom, conflicting, and unknown names", () => {
    const pending = assistant({
      id: "pending",
      agent_name_repair_state: "pending",
    });
    const rows = [
      pending,
      assistant({ agent_name_repair_state: "not_needed" }),
      assistant({ agent_name_repair_state: "conflict" }),
      assistant(),
    ];
    expect(migrationCandidates(rows, "names")).toEqual([pending]);
    expect(migrationCounts(rows).conflicts).toBe(1);
  });

  it("requires a never-configured identity and an active creator", () => {
    const eligible = assistant({ identity_state: "NEVER_CONFIGURED" });
    const rows = [
      eligible,
      assistant({ identity_state: "NEVER_CONFIGURED", creator_active: false }),
      assistant(),
      assistant({ identity_state: "UNAVAILABLE" }),
    ];
    expect(migrationCandidates(rows, "identity")).toEqual([eligible]);
    expect(migrationCounts(rows)).toMatchObject({
      identity: 1,
      needsOwner: 1,
      unavailable: 1,
    });
  });

  it("offers Kit only for runtime 2 with an active identity", () => {
    const eligible = assistant();
    const rows = [
      eligible,
      assistant({ runtime_version: 3 }),
      assistant({ runtime_version: 1 }),
      assistant({ identity_state: "NEVER_CONFIGURED" }),
      assistant({ identity_state: "UNAVAILABLE" }),
    ];
    expect(migrationCandidates(rows, "kit")).toEqual([eligible]);
    expect(migrationCounts(rows).kit).toBe(1);
  });

  it("returns zero counts for an empty organization", () => {
    expect(migrationCounts([])).toEqual({
      names: 0,
      identity: 0,
      kit: 0,
      conflicts: 0,
      needsOwner: 0,
      unavailable: 0,
    });
  });
});
