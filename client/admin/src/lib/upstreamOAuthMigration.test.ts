import { describe, expect, it } from "vitest";

import type { AdminUpstreamOAuthMigrationStatus } from "@gram/admin-client/models/components/adminupstreamoauthmigrationstatus";

import { needsMigrationRun } from "./upstreamOAuthMigration";

function status(
  migrated: boolean,
  authClientsComplete: boolean | undefined,
  unresolved = 0,
): AdminUpstreamOAuthMigrationStatus {
  return {
    organizationId: "org",
    migrated,
    servers: 1,
    pendingAssociations: 0,
    unresolvedServers: [],
    credentials: 0,
    pendingSelections: 0,
    reconnectCredentials: 0,
    authClients:
      authClientsComplete === undefined
        ? undefined
        : {
            complete: authClientsComplete,
            associations: 1,
            pendingClients: authClientsComplete ? 0 : 1,
            pendingLinks: 0,
            pendingRetirements: 0,
            pendingMarkedServers: 0,
            unresolved,
            unresolvedAssociations: [],
          },
  };
}

describe("needsMigrationRun", () => {
  it("includes a migrated organization whose auth clients are incomplete", () => {
    expect(needsMigrationRun(status(true, false))).toBe(true);
  });

  it("includes an organization that is not migrated", () => {
    expect(needsMigrationRun(status(false, true))).toBe(true);
  });

  it("skips a migrated organization whose auth clients are recorded", () => {
    expect(needsMigrationRun(status(true, true))).toBe(false);
  });

  it("skips an organization whose only auth client gaps are unresolved", () => {
    const s = status(true, false, 2);
    if (s.authClients) s.authClients.pendingClients = 0;
    expect(needsMigrationRun(s)).toBe(false);
  });

  it("includes an organization with servers to mark", () => {
    const s = status(true, false);
    if (s.authClients) {
      s.authClients.pendingClients = 0;
      s.authClients.pendingMarkedServers = 1;
    }
    expect(needsMigrationRun(s)).toBe(true);
  });

  it("decides on the migration alone when auth clients could not be checked", () => {
    expect(needsMigrationRun(status(true, undefined))).toBe(false);
    expect(needsMigrationRun(status(false, undefined))).toBe(true);
  });

  it("skips an organization whose status has not loaded", () => {
    expect(needsMigrationRun(undefined)).toBe(false);
  });
});
