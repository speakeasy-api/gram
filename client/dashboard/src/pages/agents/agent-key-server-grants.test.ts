import { describe, expect, it } from "vitest";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import {
  buildRequestedGrants,
  requestNarrowsPolicy,
} from "./agent-api-key-grants";
import { narrowGrantsToServers } from "./agent-key-server-grants";
const broad: AgentPolicyGrantForm = {
  effect: "allow",
  scope: "mcp:connect",
  selector: { resourceKind: "mcp", resourceId: "*" },
};
const first = {
  resourceId: "toolset_one",
  projectId: "project_one",
  kind: "Hosted",
};
const second = {
  resourceId: "remote_two",
  projectId: "project_two",
  kind: "Remote",
};
describe("credential server grant narrowing", () => {
  it("keeps expanded tools on selected servers and excludes deselected servers", () => {
    const requested = buildRequestedGrants(
      narrowGrantsToServers([broad], [first, second]).map((grant) => ({
        grant,
        narrowing: {
          tools: ["search", "lookup"],
          disposition: "read_only" as const,
        },
      })),
    );
    const retained = narrowGrantsToServers(requested, [second]);
    expect(retained.map((grant) => grant.selector)).toEqual(
      ["search", "lookup"].map((tool) => ({
        resourceKind: "mcp",
        resourceId: second.resourceId,
        projectId: second.projectId,
        tool,
        disposition: "read_only",
      })),
    );
    expect(narrowGrantsToServers(requested, [])).toEqual([]);
  });
  it("expands wildcard grants to exact selected resource/project pairs", () => {
    const result = narrowGrantsToServers([broad], [first, second]);
    expect(result.map((g) => g.selector)).toEqual([
      {
        resourceKind: "mcp",
        resourceId: "toolset_one",
        projectId: "project_one",
      },
      {
        resourceKind: "mcp",
        resourceId: "remote_two",
        projectId: "project_two",
      },
    ]);
    for (const grant of result)
      expect(requestNarrowsPolicy(broad.selector, grant.selector)).toBe(true);
  });
  it("excludes deselected servers, non-MCP resources and unproxied servers", () => {
    const unrelated: AgentPolicyGrantForm = {
      ...broad,
      scope: "project:read",
      selector: { resourceKind: "project", resourceId: "*" },
    };
    const pinned = {
      ...broad,
      selector: { ...broad.selector, resourceId: first.resourceId },
    };
    expect(narrowGrantsToServers([broad, pinned, unrelated], [second])).toEqual(
      [
        {
          ...broad,
          selector: {
            resourceKind: "mcp",
            resourceId: second.resourceId,
            projectId: second.projectId,
          },
        },
      ],
    );
    expect(narrowGrantsToServers([broad], [])).toEqual([]);
    expect(
      narrowGrantsToServers([broad], [{ ...first, kind: "Unproxied" }]),
    ).toEqual([]);
  });
  it("preserves pinned projects, tools, dispositions and scopes", () => {
    const pinned: AgentPolicyGrantForm = {
      ...broad,
      selector: {
        ...broad.selector,
        projectId: first.projectId,
        tool: "search",
        disposition: "read_only",
      },
    };
    expect(narrowGrantsToServers([pinned], [second])).toEqual([]);
    expect(narrowGrantsToServers([pinned], [first])).toEqual([
      {
        ...pinned,
        selector: { ...pinned.selector, resourceId: first.resourceId },
      },
    ]);
  });
  it("leaves fine-grained tool and disposition narrowing to the existing validator", () => {
    const [candidate] = narrowGrantsToServers([broad], [first]);
    expect(candidate).toBeDefined();
    const [requested] = buildRequestedGrants([
      {
        grant: candidate!,
        narrowing: {
          tool: "search",
          disposition: "read_only",
          resourceId: "other",
          projectId: "other",
        },
      },
    ]);
    expect(requested?.selector).toEqual({
      resourceKind: "mcp",
      resourceId: first.resourceId,
      projectId: first.projectId,
      tool: "search",
      disposition: "read_only",
    });
  });
  it("deduplicates overlapping candidates and servers sharing a toolset resource", () => {
    const pinned: AgentPolicyGrantForm = {
      ...broad,
      selector: {
        ...broad.selector,
        resourceId: first.resourceId,
        projectId: first.projectId,
      },
    };
    expect(narrowGrantsToServers([broad, pinned], [first, first])).toEqual([
      pinned,
    ]);
  });
});
