import { describe, expect, it } from "vitest";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";

import {
  buildRequestedGrants,
  canNarrowResource,
  openDimensions,
  requestNarrowsPolicy,
  resourceInventoryFor,
} from "./agent-api-key-grants";

function policyGrant(
  id: string,
  scope: string,
  selector: AgentPolicyGrant["selector"],
): AgentPolicyGrant {
  return {
    id,
    scope,
    effect: "allow",
    selector,
    createdAt: new Date(),
    updatedAt: new Date(),
  };
}

const anyServer = policyGrant("grant_any", "mcp:connect", {
  resourceKind: "mcp",
  resourceId: "*",
});
const pinnedTool = policyGrant("grant_tool", "mcp:write", {
  resourceKind: "mcp",
  resourceId: "server_one",
  tool: "search",
});

describe("delegated grant narrowing", () => {
  it("offers only the dimensions the server accepts for the resource kind", () => {
    expect(openDimensions(anyServer)).toEqual([
      "projectId",
      "disposition",
      "tool",
    ]);
    expect(
      openDimensions(
        policyGrant("grant_env", "environment:read", {
          resourceKind: "environment",
          resourceId: "env_one",
        }),
      ),
    ).toEqual(["projectId"]);
    expect(
      openDimensions(
        policyGrant("grant_project", "project:read", {
          resourceKind: "project",
          resourceId: "*",
        }),
      ),
    ).toEqual([]);
  });
  it("never offers a dimension the agent policy already pins", () => {
    expect(openDimensions(pinnedTool)).toEqual(["projectId", "disposition"]);
  });
  it("only offers a resource choice for a wildcard over an enumerable kind", () => {
    expect(canNarrowResource(anyServer)).toBe(true);
    expect(canNarrowResource(pinnedTool)).toBe(false);
    expect(
      canNarrowResource(
        policyGrant("grant_risk", "risk_policy:evaluate", {
          resourceKind: "risk_policy",
          resourceId: "*",
        }),
      ),
    ).toBe(false);
    expect(resourceInventoryFor("skill")).toBe("project");
    expect(resourceInventoryFor("org")).toBeNull();
  });
  it("delegates a grant unchanged when nothing is narrowed", () => {
    expect(buildRequestedGrants([{ grant: anyServer, narrowing: {} }])).toEqual(
      [
        {
          effect: "allow",
          scope: "mcp:connect",
          selector: { resourceKind: "mcp", resourceId: "*" },
        },
      ],
    );
  });
  it("applies every narrowed dimension the policy leaves open", () => {
    expect(
      buildRequestedGrants([
        {
          grant: anyServer,
          narrowing: {
            resourceId: "server_one",
            tool: "search",
            disposition: "read_only",
            projectId: "project_one",
          },
        },
      ]),
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_one",
          tool: "search",
          disposition: "read_only",
          projectId: "project_one",
        },
      },
    ]);
  });
  it("ignores narrowing that would replace what the policy pinned", () => {
    const [form] = buildRequestedGrants([
      {
        grant: pinnedTool,
        narrowing: { resourceId: "server_two", tool: "delete" },
      },
    ]);
    expect(form?.selector).toEqual({
      resourceKind: "mcp",
      resourceId: "server_one",
      tool: "search",
    });
  });
  it("rejects a request the live policy grant would not cover", () => {
    expect(
      requestNarrowsPolicy(
        { resourceKind: "mcp", resourceId: "*", tool: "search" },
        { resourceKind: "mcp", resourceId: "server_one" },
      ),
    ).toBe(false);
    expect(
      requestNarrowsPolicy(
        { resourceKind: "mcp", resourceId: "server_one" },
        { resourceKind: "mcp", resourceId: "*" },
      ),
    ).toBe(false);
    expect(
      requestNarrowsPolicy(
        { resourceKind: "mcp", resourceId: "*" },
        { resourceKind: "mcp", resourceId: "server_one", tool: "search" },
      ),
    ).toBe(true);
  });
  it("rejects two selections that resolve to the same grant", () => {
    expect(() =>
      buildRequestedGrants([
        { grant: anyServer, narrowing: { resourceId: "server_one" } },
        {
          grant: policyGrant("grant_other", "mcp:connect", {
            resourceKind: "mcp",
            resourceId: "*",
          }),
          narrowing: { resourceId: "server_one" },
        },
      ]),
    ).toThrow(/identical/);
  });
});
