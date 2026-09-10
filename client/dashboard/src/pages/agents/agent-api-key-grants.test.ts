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
    expect(openDimensions(anyServer)).toEqual(["disposition", "tool"]);
    expect(
      openDimensions(
        policyGrant("grant_env", "environment:read", {
          resourceKind: "environment",
          resourceId: "env_one",
        }),
      ),
    ).toEqual([]);
    expect(
      openDimensions(
        policyGrant("grant_project", "project:read", {
          resourceKind: "project",
          resourceId: "*",
        }),
      ),
    ).toEqual([]);
  });
  it("never offers a dimension the candidate pins to a concrete value", () => {
    expect(openDimensions(pinnedTool)).toEqual(["disposition"]);
  });
  it("treats a wildcard dimension as narrowable, not as pinned", () => {
    const wildcardDimensions = policyGrant("grant_wild", "mcp:connect", {
      resourceKind: "mcp",
      resourceId: "server_one",
      tool: "*",
      projectId: "*",
    });
    expect(openDimensions(wildcardDimensions)).toEqual(["disposition", "tool"]);
    const [form] = buildRequestedGrants([
      {
        grant: wildcardDimensions,
        narrowing: { tool: "search", projectId: "project_one" },
      },
    ]);
    expect(form?.selector).toEqual({
      resourceKind: "mcp",
      resourceId: "server_one",
      tool: "search",
      projectId: "*",
    });
    expect(
      requestNarrowsPolicy(wildcardDimensions.selector, form!.selector),
    ).toBe(true);
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
  it("expands multiple tools and dispositions as intersecting unions", () => {
    const forms = buildRequestedGrants([
      {
        grant: anyServer,
        narrowing: {
          tools: ["search", "fetch"],
          dispositions: ["read_only", "idempotent"],
        },
      },
    ]);
    expect(
      forms.map(({ selector }) => [selector.tool, selector.disposition]),
    ).toEqual([
      ["search", "read_only"],
      ["search", "idempotent"],
      ["fetch", "read_only"],
      ["fetch", "idempotent"],
    ]);
  });
  it("preserves every candidate ceiling while expanding and ignores new project narrowing", () => {
    const grant = policyGrant("restricted", "mcp:connect", {
      resourceKind: "mcp",
      resourceId: "server_one",
      projectId: "project_one",
      tool: "search",
      serverIdentity: "identity",
      serverUrl: "https://example.com/mcp",
    });
    const forms = buildRequestedGrants([
      {
        grant,
        narrowing: {
          resourceId: "other",
          projectId: "other",
          tools: ["delete", "fetch"],
          dispositions: ["read_only", "idempotent"],
        },
      },
    ]);
    expect(forms).toHaveLength(2);
    for (const form of forms) {
      expect(form.selector).toMatchObject(grant.selector);
      expect(requestNarrowsPolicy(grant.selector, form.selector)).toBe(true);
    }
    expect(
      buildRequestedGrants([
        { grant: anyServer, narrowing: { projectId: "other" } },
      ])[0]?.selector.projectId,
    ).toBeUndefined();
  });
  it("does not turn empty selections or deny grants into unrestricted allows", () => {
    expect(() =>
      buildRequestedGrants([{ grant: anyServer, narrowing: { tools: [] } }]),
    ).toThrow(/at least one/);
    expect(() =>
      buildRequestedGrants([
        { grant: anyServer, narrowing: { dispositions: [] } },
      ]),
    ).toThrow(/at least one/);
    expect(() =>
      buildRequestedGrants([
        {
          grant: {
            ...anyServer,
            effect: "deny",
          } as unknown as typeof anyServer,
          narrowing: {},
        },
      ]),
    ).toThrow(/Only allowed/);
  });
  it("deduplicates repeated values within one multi-selection", () => {
    expect(
      buildRequestedGrants([
        { grant: anyServer, narrowing: { tools: ["search", "search"] } },
      ]),
    ).toHaveLength(1);
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
