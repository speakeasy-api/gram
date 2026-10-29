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
  it("maps inventory without inventing a directory profile, membership, accounts, or activity", () => {
    const identity = registeredAgentIdentity(agent);

    expect(identity).toEqual({
      id: "agent:agent_example",
      registeredAgentId: agent.id,
      name: agent.name,
      email: "",
      role: "",
      inDirectory: false,
      seen: false,
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
  it("returns one server-decided page and forwards the abort signal", async () => {
    const client = new Gram();
    const list = page(client, { items: [agent], nextCursor: "next" });
    const signal = new AbortController().signal;

    await expect(
      fetchRegisteredAgents(
        client,
        { cursor: "here", limit: 25, search: "  bot  ", sortOrder: "desc" },
        signal,
      ),
    ).resolves.toEqual({ items: [agent], nextCursor: "next" });
    // The search is trimmed but not otherwise interpreted: matching is the
    // server's, so the browser must not narrow the page it asked for.
    expect(list).toHaveBeenCalledExactlyOnceWith(
      { cursor: "here", limit: 25, search: "bot", nameOrder: "desc" },
      undefined,
      { signal },
    );
  });

  it("asks for the first page when given no request", async () => {
    const client = new Gram();
    const list = page(client, { items: [] });

    await expect(fetchRegisteredAgents(client)).resolves.toEqual({ items: [] });
    expect(list).toHaveBeenCalledExactlyOnceWith(
      {
        cursor: undefined,
        limit: undefined,
        search: undefined,
        nameOrder: undefined,
      },
      undefined,
      { signal: undefined },
    );
  });

  it("sends no search when the caller's query is only whitespace", async () => {
    const client = new Gram();
    const list = page(client, { items: [] });

    await fetchRegisteredAgents(client, { search: "   " });
    expect(list).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ search: undefined }),
      undefined,
      { signal: undefined },
    );
  });

  it("treats the backend rollout gate's SDK 404 as an empty inventory", async () => {
    const client = new Gram();
    vi.spyOn(client.agents, "list").mockRejectedValue(httpError(404));

    await expect(fetchRegisteredAgents(client)).resolves.toEqual({ items: [] });
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

/** The SDK hands back a page object, not the array it used to return. */
function page(
  client: Gram,
  result: { items: unknown[]; nextCursor?: string },
): ReturnType<typeof vi.spyOn> {
  return vi
    .spyOn(client.agents, "list")
    .mockResolvedValue({ result } as never) as ReturnType<typeof vi.spyOn>;
}

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
    expect(matchesIdentityTelemetryFilters(inventory, {}, undefined)).toBe(
      true,
    );
  });
  it.each([
    { directory: "in_directory" },
    { directory: "not_in_directory" },
    { membership: "member" },
    { membership: "not_member" },
    { seen: "seen" },
    { seen: "not_seen" },
  ])("excludes inventory from the %j fact filter", (facts) => {
    expect(matchesIdentityTelemetryFilters(inventory, facts, undefined)).toBe(
      false,
    );
  });
  it.each(["never", "7d", "30d", "older"])(
    "excludes inventory from %s activity",
    (activity) => {
      expect(matchesIdentityTelemetryFilters(inventory, {}, activity)).toBe(
        false,
      );
    },
  );
  it("answers each fact on its own", () => {
    // A member with a directory profile who has never been seen: the facts
    // disagree, and each filter answers only its own.
    const member = {
      ...inventory,
      registeredAgentId: undefined,
      id: "user_1",
      inDirectory: true,
      seen: false,
    };
    expect(
      matchesIdentityTelemetryFilters(
        member,
        { directory: "in_directory", membership: "member", seen: "not_seen" },
        undefined,
      ),
    ).toBe(true);
    expect(
      matchesIdentityTelemetryFilters(member, { seen: "seen" }, undefined),
    ).toBe(false);
    expect(
      matchesIdentityTelemetryFilters(
        member,
        { directory: "not_in_directory" },
        undefined,
      ),
    ).toBe(false);
    expect(
      matchesIdentityTelemetryFilters(
        member,
        { membership: "not_member" },
        undefined,
      ),
    ).toBe(false);

    // Unattributed usage: seen, but not a member and not in the directory.
    const usage = {
      ...member,
      id: "usage:someone@example.com",
      inDirectory: false,
      seen: true,
    };
    expect(
      matchesIdentityTelemetryFilters(
        usage,
        {
          directory: "not_in_directory",
          membership: "not_member",
          seen: "seen",
        },
        undefined,
      ),
    ).toBe(true);
    expect(
      matchesIdentityTelemetryFilters(
        usage,
        { membership: "member" },
        undefined,
      ),
    ).toBe(false);
  });
  it("preserves activity filtering for known identities", () => {
    const person = { ...inventory, registeredAgentId: undefined };
    expect(matchesIdentityTelemetryFilters(person, {}, "never")).toBe(true);
    expect(matchesIdentityTelemetryFilters(person, {}, "7d")).toBe(false);
    const active = { ...person, lastActivityTimestamp: Date.now() };
    expect(matchesIdentityTelemetryFilters(active, {}, "7d")).toBe(true);
    expect(matchesIdentityTelemetryFilters(active, {}, "never")).toBe(false);
  });
});
