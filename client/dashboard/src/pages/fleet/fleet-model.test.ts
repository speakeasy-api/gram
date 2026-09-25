import { describe, expect, it } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import {
  buildFleetRows,
  fleetDirectory,
  recentFleetRows,
  FLEET_WINDOW_MS,
} from "./fleet-model";

const now = new Date("2026-09-01T12:00:00Z");
const agent: ManagedAgent = {
  id: "same-id",
  name: "Release assistant",
  ownerUserId: "owner",
  lifecycle: "active",
  createdAt: now,
  updatedAt: now,
  permissions: { read: true, write: true, transfer: true, authorize: true },
};
const assistant: Assistant = {
  id: "same-id",
  name: agent.name,
  projectId: "project",
  createdByUserId: "creator",
  createdAt: now,
  updatedAt: now,
  instructions: "Synthetic instructions",
  model: "example",
  status: "active",
  maxConcurrency: 1,
  warmTtlSeconds: 60,
  mcpServers: [],
  toolsets: [],
  skills: [],
};
const session: ChatOverview = {
  id: "same-id",
  title: agent.name,
  assistantId: assistant.id,
  userId: "capturer",
  createdAt: now,
  updatedAt: now,
  lastMessageTimestamp: now,
  numMessages: 2,
};
const input = {
  agents: [agent],
  assistants: [assistant],
  sessions: [session],
  members: [],
  projectId: "project",
};

describe("Fleet attribution", () => {
  it("groups explicit people by directory department and retains loaded assistant evidence", () => {
    const rows = buildFleetRows({
      ...input,
      sessions: [],
      members: [
        {
          id: "owner",
          name: "Synthetic owner",
          email: "owner@example.test",
          department: "Engineering",
          joinedAt: now,
          principalUrn: "urn:gram:principal:user:owner",
          roleIds: [],
        },
      ],
      observedAssistantActivity: new Map([[assistant.id, now]]),
    });
    expect(rows[0]).toMatchObject({
      personName: "Synthetic owner",
      department: "Engineering",
    });
    expect(rows[1]).toMatchObject({
      personName: "Directory profile unavailable",
      department: "Unassigned department",
      lastActivity: now,
    });
    const engineering = fleetDirectory(rows).find(
      (department) => department.name === "Engineering",
    );
    expect(engineering?.identities).toMatchObject([
      {
        id: "user:owner",
        name: "Synthetic owner",
        roles: ["Owner"],
        rows: [{ id: "agent:same-id" }],
      },
    ]);
  });
  it("keeps namespaces and person roles distinct despite identical names and ids", () => {
    const rows = buildFleetRows(input);
    expect(rows.map((row) => row.id)).toEqual([
      "agent:same-id",
      "assistant:same-id",
      "session:same-id",
    ]);
    expect(rows.map((row) => [row.personRole, row.personId])).toEqual([
      ["Owner", "owner"],
      ["Creator", "creator"],
      ["Captured user", "capturer"],
    ]);
    expect(rows[2]?.ownerId).toBeUndefined();
    expect(rows[2]?.createdById).toBeUndefined();
    expect(rows[2]?.agent).toBeUndefined();
    expect(rows[2]?.assistant?.id).toBe(assistant.id);
  });
  it("never inherits an assistant creator when capture user is missing", () => {
    const rows = buildFleetRows({
      ...input,
      sessions: [
        { ...session, userId: undefined },
        { ...session, id: "another", userId: undefined },
      ],
    });
    const captures = rows.filter((row) => row.source === "session");
    expect(
      captures.every(
        (row) => !row.personId && row.personName === "Unknown attribution",
      ),
    ).toBe(true);
    expect(fleetDirectory(captures)[0]?.identities).toHaveLength(2);
  });
  it("includes identities without captures while excluding other projects", () => {
    const rows = buildFleetRows({
      ...input,
      agents: [agent, { ...agent, id: "elsewhere", projectId: "other" }],
      sessions: [],
    });
    expect(rows.map((row) => row.id)).toEqual([
      "agent:same-id",
      "assistant:same-id",
    ]);
    expect(rows[0]?.lastActivity).toBeUndefined();
  });
  it("uses only explicit assistant captures for last activity", () => {
    const rows = buildFleetRows(input);
    expect(
      rows.find((row) => row.source === "assistant")?.lastActivity,
    ).toEqual(now);
    expect(
      rows.find((row) => row.source === "agent")?.lastActivity,
    ).toBeUndefined();
  });
  it("keeps unverified external captures separate from equal user and agent identifiers", () => {
    const rows = buildFleetRows({
      ...input,
      agents: [{ ...agent, id: "shared", ownerUserId: "shared" }],
      sessions: [
        {
          ...session,
          id: "external",
          userId: undefined,
          externalUserId: "shared",
        },
        { ...session, id: "directory", userId: "shared" },
      ],
    });
    const external = rows.find((row) => row.id === "session:external");
    expect(external?.externalCaptureUserId).toBe("shared");
    expect(external?.personName).toBe("External user (unverified) · shared");
    expect(external?.personId).toBeUndefined();
    expect(external?.ownerId).toBeUndefined();
    expect(external?.agent).toBeUndefined();
    const branches = fleetDirectory(rows).flatMap(
      (department) => department.identities,
    );
    expect(
      branches
        .find((branch) => branch.id === "external:shared")
        ?.rows.map((row) => row.id),
    ).toEqual(["session:external"]);
    expect(
      branches
        .find((branch) => branch.id === "user:shared")
        ?.rows.map((row) => row.id),
    ).toEqual(["agent:shared", "session:directory"]);
  });
});

describe("Fleet observed activity window", () => {
  it("includes the boundary and server clock skew, excludes older and unknown activity", () => {
    const boundary = new Date(now.getTime() - FLEET_WINDOW_MS);
    const rows = buildFleetRows({
      ...input,
      sessions: [],
      assistants: [],
      agents: [
        { ...agent, id: "boundary", lastCredentialUsedAt: boundary },
        {
          ...agent,
          id: "old",
          lastCredentialUsedAt: new Date(boundary.getTime() - 1),
        },
        {
          ...agent,
          id: "ahead",
          lastCredentialUsedAt: new Date(now.getTime() + 60_000),
        },
        { ...agent, id: "profile-only" },
      ],
    });
    const recent = recentFleetRows(rows, now.getTime());
    expect(recent.map((row) => row.id)).toEqual([
      "agent:boundary",
      "agent:ahead",
    ]);
    expect(
      fleetDirectory(recent).flatMap((department) =>
        department.identities.flatMap((identity) =>
          identity.rows.map((row) => row.id),
        ),
      ),
    ).toEqual(["agent:boundary", "agent:ahead"]);
  });
  it("uses only explicit loaded captures for assistants after paging or searching", () => {
    const firstPage = recentFleetRows(buildFleetRows(input), now.getTime());
    expect(firstPage.map((row) => row.id)).toEqual([
      "assistant:same-id",
      "session:same-id",
    ]);
    const otherPage = recentFleetRows(
      buildFleetRows({
        ...input,
        sessions: [{ ...session, id: "other", assistantId: undefined }],
      }),
      now.getTime(),
    );
    expect(otherPage.map((row) => row.id)).toEqual(["session:other"]);
    const noSearchMatches = recentFleetRows(
      buildFleetRows({ ...input, sessions: [] }),
      now.getTime(),
    );
    expect(noSearchMatches).toEqual([]);
  });
});
