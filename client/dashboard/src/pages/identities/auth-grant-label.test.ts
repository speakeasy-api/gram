import { describe, expect, it } from "vitest";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
import {
  authGrantParts,
  authGrantRank,
  authGrantText,
  REPRESENTED_USER,
  type AuthGrantNames,
} from "./auth-grant-label";

const grainServer = {
  id: "server-grain",
  slug: "grain",
  name: "Grain",
  projectId: "project-default",
};
const calendarServer = {
  id: "server-calendar",
  slug: "calendar",
  name: "Content calendar",
  projectId: "project-default",
};

const otherProjectServer = {
  id: "server-other",
  slug: "other",
  name: "Other calendar",
  projectId: "project-other",
};

const clients: Record<
  string,
  { name: string; clientType: string; servers: (typeof grainServer)[] }
> = {
  "client-shared": {
    name: "Calendar token",
    clientType: "service_account",
    servers: [calendarServer, otherProjectServer],
  },
  "client-grain": {
    name: "Grain sign-in",
    clientType: "remote_session",
    servers: [grainServer],
  },
};

const names: AuthGrantNames = {
  client: (id) => clients[id],
  upstreamClient: (id) =>
    id === "upstream-grain"
      ? { name: "Grain", issuerId: "issuer-grain", servers: [grainServer] }
      : undefined,
  member: (id) => (id === "user-ada" ? "Ada Lovelace" : undefined),
  clientsLoaded: true,
};

function selector(fields: Partial<AgentPolicySelector>): AgentPolicySelector {
  return { resourceKind: "mcp_auth_client", resourceId: "*", ...fields };
}

function label(fields: Partial<AgentPolicySelector>): string {
  return authGrantText(authGrantParts(selector(fields), names));
}

describe("authGrantParts", () => {
  it.each([
    {
      case: "an unrestricted grant",
      fields: {},
      want: "Any upstream credential in the organization",
    },
    {
      case: "a wildcard connection owner as unrestricted",
      fields: { connectionOwner: "*" },
      want: "Any upstream credential in the organization",
    },
    {
      case: "one credential by name and type, with the servers it serves",
      fields: { resourceId: "client-shared" },
      want: "Calendar token · Service account · on Content calendar, Other calendar",
    },
    {
      case: "one credential narrowed to a project, with only that project's servers",
      fields: { resourceId: "client-shared", projectId: "project-default" },
      want: "Calendar token · Service account · on Content calendar",
    },
    {
      case: "one person's connection to one credential",
      fields: { resourceId: "client-grain", connectionOwner: "user-ada" },
      want: "Ada Lovelace's connection to Grain sign-in · Each person's own account · on Grain",
    },
    {
      case: "the represented user's connection to one credential",
      fields: { resourceId: "client-grain", connectionOwner: REPRESENTED_USER },
      want: "The connection of the person the agent acts for to Grain sign-in · Each person's own account · on Grain",
    },
    {
      case: "one credential through one upstream client",
      fields: { resourceId: "client-grain", upstreamClient: "upstream-grain" },
      want: "Grain sign-in · Each person's own account · through upstream client Grain · on Grain",
    },
    {
      case: "one person's connection to a credential that is gone",
      fields: {
        resourceId: "63990be4-0000-4000-8000-000000000000",
        connectionOwner: "user-ada",
      },
      want: "Ada Lovelace's connection to upstream credential 63990be4…, no longer found",
    },
    {
      case: "an upstream client's servers narrowed to another project",
      fields: { upstreamClient: "upstream-grain", projectId: "project-other" },
      want: "Any personal connection to Grain",
    },
    {
      case: "a credential that is gone",
      fields: { resourceId: "63990be4-0000-4000-8000-000000000000" },
      want: "Upstream credential 63990be4…, no longer found",
    },
    {
      case: "the represented user's own connections",
      fields: {
        clientType: "remote_session" as const,
        connectionOwner: REPRESENTED_USER,
      },
      want: "Personal connections of the person the agent acts for",
    },
    {
      case: "the represented user's connection to one upstream",
      fields: {
        connectionOwner: REPRESENTED_USER,
        upstreamClient: "upstream-grain",
      },
      want: "The Grain connection of the person the agent acts for · on Grain",
    },
    {
      case: "one person's connection through one upstream client",
      fields: { connectionOwner: "user-ada", upstreamClient: "upstream-grain" },
      want: "Ada Lovelace's Grain connection · on Grain",
    },
    {
      case: "one person's connections",
      fields: { connectionOwner: "user-ada" },
      want: "Ada Lovelace's personal connections",
    },
    {
      case: "an unknown person and upstream client by short ids",
      fields: {
        connectionOwner: "d265f01b-0000-4000-8000-000000000000",
        upstreamClient: "01a10332-0000-4000-8000-000000000000",
      },
      want: "User d265f01b…'s connection through upstream client 01a10332…",
    },
    {
      case: "everyone's connections to one upstream",
      fields: { upstreamClient: "upstream-grain" },
      want: "Any personal connection to Grain · on Grain",
    },
    {
      case: "every personal connection",
      fields: { clientType: "remote_session" as const },
      want: "Any personal connection in the organization",
    },
    {
      case: "every credential of another type",
      fields: { clientType: "service_account" as const },
      want: "Any service account in the organization",
    },
  ])("names $case", ({ fields, want }) => {
    expect(label(fields)).toBe(want);
  });

  it("does not call a credential gone before the credentials load", () => {
    const parts = authGrantParts(
      selector({ resourceId: "63990be4-0000-4000-8000-000000000000" }),
      { ...names, clientsLoaded: false },
    );
    expect(authGrantText(parts)).toBe("Upstream credential 63990be4…");
  });

  it("keeps the person in a grant on one credential so it can be linked", () => {
    const parts = authGrantParts(
      selector({ resourceId: "client-grain", connectionOwner: "user-ada" }),
      names,
    );
    expect(parts[0]).toEqual({
      kind: "person",
      userId: "user-ada",
      name: "Ada Lovelace",
    });
  });

  it("keeps a named person and the servers apart so they can be linked", () => {
    const parts = authGrantParts(
      selector({
        connectionOwner: "user-ada",
        upstreamClient: "upstream-grain",
      }),
      names,
    );
    expect(parts[0]).toEqual({
      kind: "person",
      userId: "user-ada",
      name: "Ada Lovelace",
    });
    expect(parts[2]).toEqual({
      kind: "upstream",
      id: "upstream-grain",
      name: "Grain",
      issuerId: "issuer-grain",
    });
    expect(parts.at(-1)).toEqual({ kind: "servers", servers: [grainServer] });
  });

  it("still links a person and an upstream client it cannot name", () => {
    const parts = authGrantParts(
      selector({
        connectionOwner: "d265f01b-0000-4000-8000-000000000000",
        upstreamClient: "01a10332-0000-4000-8000-000000000000",
      }),
      names,
    );
    expect(parts[0]).toEqual({
      kind: "person",
      userId: "d265f01b-0000-4000-8000-000000000000",
      name: "User d265f01b…",
    });
    expect(parts.at(-1)).toEqual({
      kind: "upstream",
      id: "01a10332-0000-4000-8000-000000000000",
      name: "01a10332…",
      issuerId: undefined,
    });
  });
});

describe("authGrantRank", () => {
  it("orders grants from every credential down to one named credential", () => {
    const ordered = [
      selector({ resourceId: "client-shared" }),
      selector({
        connectionOwner: "user-ada",
        upstreamClient: "upstream-grain",
      }),
      selector({ connectionOwner: "user-ada" }),
      selector({
        connectionOwner: REPRESENTED_USER,
        upstreamClient: "upstream-grain",
      }),
      selector({ connectionOwner: REPRESENTED_USER }),
      selector({ upstreamClient: "upstream-grain" }),
      selector({ clientType: "remote_session" }),
      selector({ clientType: "remote_session", projectId: "project-default" }),
      selector({}),
    ].sort((a, b) => authGrantRank(a) - authGrantRank(b));
    expect(
      ordered.map((grant) => authGrantText(authGrantParts(grant, names))),
    ).toEqual([
      "Any upstream credential in the organization",
      "Any personal connection in the organization",
      "Any personal connection in the organization",
      "Any personal connection to Grain · on Grain",
      "Personal connections of the person the agent acts for",
      "The Grain connection of the person the agent acts for · on Grain",
      "Ada Lovelace's personal connections",
      "Ada Lovelace's Grain connection · on Grain",
      "Calendar token · Service account · on Content calendar, Other calendar",
    ]);
    expect(
      authGrantRank(selector({ clientType: "remote_session", projectId: "p" })),
    ).toBeGreaterThan(
      authGrantRank(selector({ clientType: "remote_session" })),
    );
  });
});
