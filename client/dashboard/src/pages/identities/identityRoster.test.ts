import { Gram } from "@gram/client";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { afterEach, describe, expect, it, vi } from "vitest";
import { identityHasAccount, identityKindOf } from "./identityKind";
import {
  fetchRegisteredAgents,
  registeredAgentIdentity,
  registeredAgentHref,
  matchesIdentityTelemetryFilters,
} from "./identityRoster";

const agent: ManagedAgent = {
  id: "agent_example",
  name: "Automation@example.com",
  ownerUserId: "user_owner",
  lifecycle: "active",
  permissions: { read: true, write: false, authorize: true, transfer: false },
  createdAt: new Date("2026-01-01T00:00:00Z"),
  updatedAt: new Date("2026-01-01T00:00:00Z"),
};

afterEach(() => vi.restoreAllMocks());

describe("registeredAgentIdentity", () => {
  it("maps inventory without inventing human enrollment, accounts, or activity", () => {
    const identity = registeredAgentIdentity(agent);

    expect(identity).toEqual({
      id: "agent:agent_example",
      registeredAgentId: agent.id,
      name: agent.name,
      email: "",
      role: "",
      status: "not_enrolled",
      tokenCount: 0,
      lastActivity: "—",
      lastActivityTimestamp: null,
      accounts: [],
      mostRecentAccount: null,
      hasPersonalAccount: false,
      roleIds: [],
      department: "",
      teams: [],
    });
    expect(identityKindOf(identity)).toBe("agent");
    expect(identityHasAccount(identity)).toBe(false);
  });
});

describe("fetchRegisteredAgents", () => {
  it("returns the org-scoped inventory and forwards the abort signal", async () => {
    const client = new Gram();
    const list = vi.spyOn(client.agents, "list").mockResolvedValue([agent]);
    const signal = new AbortController().signal;

    await expect(fetchRegisteredAgents(client, signal)).resolves.toEqual([
      agent,
    ]);
    expect(list).toHaveBeenCalledExactlyOnceWith(undefined, undefined, {
      signal,
    });
  });

  it("allows callers to omit the abort signal", async () => {
    const client = new Gram();
    const list = vi.spyOn(client.agents, "list").mockResolvedValue([]);

    await expect(fetchRegisteredAgents(client)).resolves.toEqual([]);
    expect(list).toHaveBeenCalledExactlyOnceWith(undefined, undefined, {
      signal: undefined,
    });
  });

  it("treats the backend rollout gate's SDK 404 as an empty inventory", async () => {
    const client = new Gram();
    vi.spyOn(client.agents, "list").mockRejectedValue(httpError(404));

    await expect(fetchRegisteredAgents(client)).resolves.toEqual([]);
  });

  it.each([403, 500])("propagates genuine SDK %i failures", async (status) => {
    const client = new Gram();
    const error = httpError(status);
    vi.spyOn(client.agents, "list").mockRejectedValue(error);

    await expect(fetchRegisteredAgents(client)).rejects.toBe(error);
  });

  it("does not mistake a non-SDK error for the rollout gate", async () => {
    const client = new Gram();
    const error = Object.assign(new Error("Failed to list agents"), {
      statusCode: 404,
    });
    vi.spyOn(client.agents, "list").mockRejectedValue(error);

    await expect(fetchRegisteredAgents(client)).rejects.toBe(error);
  });
});

function httpError(status: number): GramError {
  return new GramError("Failed to list agents", {
    response: new Response(null, { status }),
    request: new Request("https://example.com/rpc/agents.list"),
    body: "",
  });
}

describe("registeredAgentHref", () => {
  it("preserves the time window and repeated filters while replacing only id", () => {
    const href = registeredAgentHref(
      "/org/agents",
      "?from=2026-01-01&to=2026-02-01&kind=agent&kind=unknown&id=old",
      "agent/a b",
    );
    const url = new URL(href, "https://example.com");
    expect(url.pathname).toBe("/org/agents");
    expect([...url.searchParams.entries()]).toEqual([
      ["from", "2026-01-01"],
      ["to", "2026-02-01"],
      ["kind", "agent"],
      ["kind", "unknown"],
      ["id", "agent/a b"],
    ]);
  });
  it("handles an empty query", () => {
    expect(registeredAgentHref("/org/agents", "", "agent/a")).toBe(
      "/org/agents?id=agent%2Fa",
    );
  });
});

describe("matchesIdentityTelemetryFilters", () => {
  const inventory = registeredAgentIdentity(agent);
  it("keeps inventory visible without telemetry filters", () => {
    expect(
      matchesIdentityTelemetryFilters(inventory, undefined, undefined),
    ).toBe(true);
  });
  it.each(["not_enrolled", "enrolled"])(
    "excludes inventory from %s enrollment",
    (enrollment) => {
      expect(
        matchesIdentityTelemetryFilters(inventory, enrollment, undefined),
      ).toBe(false);
    },
  );
  it.each(["never", "7d", "30d", "older"])(
    "excludes inventory from %s activity",
    (activity) => {
      expect(
        matchesIdentityTelemetryFilters(inventory, undefined, activity),
      ).toBe(false);
    },
  );
  it("preserves known non-inventory enrollment and activity filtering", () => {
    const person = { ...inventory, registeredAgentId: undefined };
    expect(
      matchesIdentityTelemetryFilters(person, "not_enrolled", "never"),
    ).toBe(true);
    expect(matchesIdentityTelemetryFilters(person, "enrolled", undefined)).toBe(
      false,
    );
    expect(matchesIdentityTelemetryFilters(person, undefined, "7d")).toBe(
      false,
    );
    const active = { ...person, lastActivityTimestamp: Date.now() };
    expect(matchesIdentityTelemetryFilters(active, undefined, "7d")).toBe(true);
    expect(matchesIdentityTelemetryFilters(active, undefined, "never")).toBe(
      false,
    );
  });
});
