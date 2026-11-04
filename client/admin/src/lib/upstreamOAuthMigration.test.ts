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
            inProgress: false,
            associations: 1,
            pendingClients: authClientsComplete ? 0 : 1,
            pendingLinks: 0,
            pendingHeaders: 0,
            pendingRetirements: 0,
            pendingMarkedServers: 0,
            unresolved,
            unresolvedAssociations: [],
            warnings: [],
            kinds: [],
            failedPhases: [],
            selectionsComplete: true,
            pendingSelectionMappings: 0,
            pendingGrants: 0,
            selectionClasses: [],
            selectionGaps: [],
            selectionGapCount: 0,
          },
  };
}

describe("needsMigrationRun", () => {
  it("reads a status from a server that predates selection coverage", () => {
    const s = status(true, true);
    if (s.authClients) {
      delete s.authClients.selectionsComplete;
      delete s.authClients.pendingSelectionMappings;
      delete s.authClients.selectionClasses;
      delete s.authClients.selectionGaps;
      delete s.authClients.selectionGapCount;
    }
    expect(needsMigrationRun(s)).toBe(false);
  });

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

  it("includes an organization with headers to update or a failed phase", () => {
    const headers = status(true, false);
    const failed = status(true, false);
    if (headers.authClients && failed.authClients) {
      headers.authClients.pendingClients = 0;
      headers.authClients.pendingHeaders = 1;
      failed.authClients.pendingClients = 0;
      failed.authClients.failedPhases = ["ema"];
    }
    expect(needsMigrationRun(headers)).toBe(true);
    expect(needsMigrationRun(failed)).toBe(true);
  });

  it("includes an organization with credential choices to map", () => {
    const s = status(true, false);
    if (s.authClients) {
      s.authClients.pendingClients = 0;
      s.authClients.pendingSelectionMappings = 1;
    }
    expect(needsMigrationRun(s)).toBe(true);
  });

  it("includes an organization with default permissions to grant", () => {
    const s = status(true, true);
    if (s.authClients) s.authClients.pendingGrants = 2;
    expect(needsMigrationRun(s)).toBe(true);
  });

  it("skips an organization whose only choice gaps need a reconnect", () => {
    const s = status(true, false);
    if (s.authClients) {
      s.authClients.pendingClients = 0;
      s.authClients.selectionsComplete = false;
      s.authClients.selectionGapCount = 3;
    }
    expect(needsMigrationRun(s)).toBe(false);
  });

  it("includes an organization with execution profiles to record", () => {
    const s = status(true, true);
    s.executionProfiles = {
      complete: false,
      targets: 1,
      pendingVersions: 2,
      gaps: [],
    };
    expect(needsMigrationRun(s)).toBe(true);
  });

  it("skips an organization whose only profile gaps need configuring", () => {
    const s = status(true, true);
    s.executionProfiles = {
      complete: false,
      targets: 1,
      pendingVersions: 0,
      gaps: [
        {
          executionContext: "userless",
          reason: "personal_requires_user",
          servers: 1,
        },
      ],
    };
    expect(needsMigrationRun(s)).toBe(false);
  });

  it("skips profiles to record while a run records auth clients", () => {
    const s = status(true, true);
    if (s.authClients) s.authClients.inProgress = true;
    s.executionProfiles = {
      complete: false,
      targets: 1,
      pendingVersions: 2,
      gaps: [],
    };
    expect(needsMigrationRun(s)).toBe(false);
  });

  it("skips an organization whose auth clients a run is recording", () => {
    const s = status(true, false);
    if (s.authClients) s.authClients.inProgress = true;
    expect(needsMigrationRun(s)).toBe(false);
  });

  it("decides on the migration alone when auth clients could not be checked", () => {
    expect(needsMigrationRun(status(true, undefined))).toBe(false);
    expect(needsMigrationRun(status(false, undefined))).toBe(true);
  });

  it("skips an organization whose status has not loaded", () => {
    expect(needsMigrationRun(undefined)).toBe(false);
  });
});
