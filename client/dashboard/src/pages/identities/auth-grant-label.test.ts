import { describe, expect, it } from "vitest";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
import {
  authGrantLabel,
  REPRESENTED_USER,
  type AuthGrantNames,
} from "./auth-grant-label";

const names: AuthGrantNames = {
  client: (id) =>
    id === "client-shared"
      ? { name: "Content calendar", clientType: "service_account" }
      : undefined,
  upstreamClient: (id) => (id === "upstream-grain" ? "Grain" : undefined),
  member: (id) => (id === "user-ada" ? "Ada Lovelace" : undefined),
};

function selector(fields: Partial<AgentPolicySelector>): AgentPolicySelector {
  return { resourceKind: "mcp_auth_client", resourceId: "*", ...fields };
}

describe("authGrantLabel", () => {
  it.each([
    {
      case: "an unrestricted grant",
      fields: {},
      want: "Every upstream credential in the organization",
    },
    {
      case: "a wildcard connection owner as unrestricted",
      fields: { connectionOwner: "*" },
      want: "Every upstream credential in the organization",
    },
    {
      case: "one shared credential by name and type",
      fields: { resourceId: "client-shared" },
      want: "Content calendar · Service account",
    },
    {
      case: "an unknown credential by its short id",
      fields: { resourceId: "63990be4-0000-4000-8000-000000000000" },
      want: "Upstream credential 63990be4…",
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
      case: "one person's connection through one upstream client",
      fields: { connectionOwner: "user-ada", upstreamClient: "upstream-grain" },
      want: "Ada Lovelace's Grain connection",
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
      want: "User d265f01b…'s upstream client 01a10332… connection",
    },
    {
      case: "every personal connection",
      fields: { clientType: "remote_session" as const },
      want: "Every personal connection in the organization",
    },
    {
      case: "every credential of another type",
      fields: { clientType: "service_account" as const },
      want: "Every service account in the organization",
    },
  ])("names $case", ({ fields, want }) => {
    expect(authGrantLabel(selector(fields), names)).toBe(want);
  });
});
