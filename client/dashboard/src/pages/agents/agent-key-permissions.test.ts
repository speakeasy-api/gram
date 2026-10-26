import { describe, expect, it } from "vitest";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { Key } from "@gram/client/models/components/key.js";
import type { ResourceLinkMaps } from "@/pages/access/useResourceLinkMaps";
import {
  isAgentWidePermission,
  permissionLabel,
  permissionsMissingFromKey,
  summarizePermissions,
} from "./agent-key-permissions";

const maps: ResourceLinkMaps = {
  projectMap: new Map([["project_one", { slug: "one", name: "Project one" }]]),
  toolsetMap: new Map([
    ["server_a", { slug: "a", name: "Linear", projectId: "project_one" }],
    ["server_b", { slug: "b", name: "GitHub", projectId: "project_one" }],
    ["server_c", { slug: "c", name: "Slack", projectId: "project_one" }],
    ["server_d", { slug: "d", name: "Notion", projectId: "project_one" }],
  ]),
  mcpServerMap: new Map(),
};
const mcp = (resourceId: string) => ({
  scope: "mcp:connect",
  selector: { resourceKind: "mcp" as const, resourceId },
});

describe("summarizePermissions", () => {
  it("names a few resources and counts the rest", () => {
    expect(summarizePermissions([mcp("server_a")], maps)).toEqual([
      "Use tools on Linear",
    ]);
    expect(
      summarizePermissions([mcp("server_a"), mcp("server_b")], maps),
    ).toEqual(["Use tools on Linear and GitHub"]);
    expect(
      summarizePermissions(
        ["server_a", "server_b", "server_c", "server_d"].map(mcp),
        maps,
      ),
    ).toEqual(["Use tools on 4 MCP servers"]);
  });
  it("counts resources it cannot name rather than showing their IDs", () => {
    expect(
      summarizePermissions([mcp("server_a"), mcp("deleted_server")], maps),
    ).toEqual(["Use tools on Linear and 1 more"]);
    expect(summarizePermissions([mcp("deleted_server")], maps)).toEqual([
      "Use tools on 1 MCP server",
    ]);
  });
  it("says a wildcard covers everything of its kind, and groups by permission", () => {
    expect(
      summarizePermissions(
        [
          mcp("server_a"),
          {
            scope: "project:read",
            selector: { resourceKind: "project", resourceId: "*" },
          },
          {
            scope: "project:blocked_read",
            selector: { resourceKind: "project", resourceId: "project_one" },
          },
        ],
        maps,
      ),
    ).toEqual(["Use tools on Linear", "View projects on all projects"]);
  });
});

describe("permissionsMissingFromKey", () => {
  const issuedAt = new Date("2026-01-10T00:00:00Z");
  const key = (requested: ReturnType<typeof mcp>[]): Key =>
    ({
      id: "key_example",
      name: "Example key",
      createdAt: issuedAt,
      delegatedGrants: { requested, effective: [] },
    }) as unknown as Key;
  const grant = (
    scope: string,
    resourceId: string,
    createdAt: Date,
    effect: "allow" | "deny" = "allow",
  ): AgentPolicyGrant =>
    ({
      id: `${scope}:${resourceId}`,
      effect,
      scope,
      selector: {
        resourceKind: scope.startsWith("mcp:") ? "mcp" : "project",
        resourceId,
      },
      createdAt,
      updatedAt: createdAt,
    }) as AgentPolicyGrant;
  const later = new Date("2026-02-01");
  const earlier = new Date("2026-01-01");

  it("returns permissions added after issuance that the key lacks", () => {
    const missing = permissionsMissingFromKey(key([mcp("server_a")]), [
      grant("mcp:connect", "server_a", later),
      grant("mcp:connect", "server_b", later),
      grant("project:read", "*", later),
      grant("environment:read", "*", earlier),
      grant("project:blocked_read", "project_one", later),
    ]);
    expect(missing.map(({ id }) => id)).toEqual([
      "mcp:connect:server_b",
      "project:read:*",
    ]);
  });
  it("treats a key's wildcard as holding every resource of that permission", () => {
    expect(
      permissionsMissingFromKey(key([mcp("*")]), [
        grant("mcp:connect", "server_b", later),
      ]),
    ).toEqual([]);
  });
  it("never flags a key that follows the agent's live permissions", () => {
    expect(
      permissionsMissingFromKey(key([]), [grant("project:read", "*", later)]),
    ).toEqual([]);
  });
});

describe("isAgentWidePermission", () => {
  it("leaves server access and blocks to the server step", () => {
    expect(isAgentWidePermission({ scope: "mcp:connect" })).toBe(false);
    expect(isAgentWidePermission({ scope: "project:blocked_read" })).toBe(
      false,
    );
    expect(
      isAgentWidePermission({ scope: "project:read", effect: "deny" }),
    ).toBe(false);
    expect(isAgentWidePermission({ scope: "project:read" })).toBe(true);
    expect(permissionLabel("project:read")).toBe("View projects");
    expect(permissionLabel("future:scope")).toBe("future:scope");
  });
});
