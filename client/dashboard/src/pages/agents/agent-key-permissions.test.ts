import { describe, expect, it } from "vitest";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
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
  const key = (
    requested: { scope: string; selector: AgentPolicySelector }[],
  ): Key =>
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
  it.each([
    { tool: "read-file" },
    { disposition: "read_only" },
    { projectId: "project_one" },
    { clientType: "remote_session" },
    { connectionOwner: "owner" },
    { upstreamClient: "client" },
    { serverIdentity: "server" },
    { serverUrl: "https://example.test/mcp" },
  ] satisfies Partial<AgentPolicySelector>[])(
    "does not mistake a narrowed selector %j for a broader grant",
    (selector) => {
      const wanted = grant("mcp:connect", "server_a", later);
      const issued = {
        ...mcp("server_a"),
        selector: { ...mcp("server_a").selector, ...selector },
      };
      expect(permissionsMissingFromKey(key([issued]), [wanted])).toEqual([
        wanted,
      ]);
    },
  );
  it("recognizes wildcard dimensions as covering a narrower grant", () => {
    const wanted = grant("mcp:connect", "server_a", later);
    wanted.selector = {
      ...wanted.selector,
      tool: "read-file",
      projectId: "project_one",
    };
    expect(
      permissionsMissingFromKey(
        key([
          {
            ...mcp("*"),
            selector: { ...mcp("*").selector, tool: "*", projectId: "*" },
          },
        ]),
        [wanted],
      ),
    ).toEqual([]);
  });
  it("reports a grant broadened after issuance even when it was created earlier", () => {
    const wanted = grant("mcp:connect", "*", earlier);
    wanted.updatedAt = later;
    expect(permissionsMissingFromKey(key([mcp("server_a")]), [wanted])).toEqual(
      [wanted],
    );
  });
  it("never flags a key that follows the agent's live permissions", () => {
    expect(
      permissionsMissingFromKey(key([]), [grant("project:read", "*", later)]),
    ).toEqual([]);
  });
});

describe("isAgentWidePermission", () => {
  it("leaves server access and blocks to the server step", () => {
    expect(
      isAgentWidePermission({
        selector: { resourceKind: "project", resourceId: "*" },
        scope: "mcp:connect",
      }),
    ).toBe(false);
    expect(
      isAgentWidePermission({
        selector: { resourceKind: "project", resourceId: "*" },
        scope: "project:blocked_read",
      }),
    ).toBe(false);
    expect(
      isAgentWidePermission({
        selector: { resourceKind: "project", resourceId: "*" },
        scope: "project:read",
        effect: "deny",
      }),
    ).toBe(false);
    expect(
      isAgentWidePermission({
        selector: { resourceKind: "project", resourceId: "*" },
        scope: "project:read",
      }),
    ).toBe(true);
    for (const scope of ["mcp:read", "mcp:write", "mcp:auth"]) {
      expect(
        isAgentWidePermission({
          scope,
          selector: { resourceKind: "mcp", resourceId: "*" },
        }),
      ).toBe(false);
    }
    expect(
      isAgentWidePermission({
        scope: "project:read",
        selector: { resourceKind: "mcp", resourceId: "*" },
      }),
    ).toBe(false);
    expect(permissionLabel("project:read")).toBe("View projects");
    expect(permissionLabel("future:scope")).toBe("future:scope");
  });
});
