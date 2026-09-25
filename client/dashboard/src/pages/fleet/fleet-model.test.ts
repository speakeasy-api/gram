import { describe, expect, it } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import { buildFleetRows, fleetDirectory } from "./fleet-model";

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
});
