import { describe, expect, it } from "vitest";
import { scopeCatalogue, workflowsNeeding } from "./scopeCatalogue";

import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { ScopeWorkflow } from "@gram/client/models/components/scopeworkflow.js";

function def(slug: string, subScopes: string[] = []): ScopeDefinition {
  return {
    slug: slug as ScopeDefinition["slug"],
    label: slug,
    description: `${slug}.`,
    resourceType: "mcp",
    category: "mcp",
    visibility: "user_visible",
    agentEligible: true,
    sensitive: false,
    subScopes: subScopes as ScopeDefinition["subScopes"],
  };
}

function workflow(slug: string, scopes: string[]): ScopeWorkflow {
  return { slug, label: slug, scopes: scopes as ScopeWorkflow["scopes"] };
}

describe("workflowsNeeding", () => {
  // The inclusions are a hand-written subset of the server's; its tests
  // guard the full graph.
  const catalogue = scopeCatalogue([
    def("mcp:connect"),
    def("mcp:read", ["mcp:connect"]),
    def("mcp:write", ["mcp:connect", "mcp:read"]),
    def("log:read"),
  ]);
  const workflows = [
    workflow("maintenance", ["mcp:write", "log:read"]),
    workflow("review", ["mcp:read", "log:read"]),
    workflow("unrelated", ["log:read"]),
  ];

  it("counts a workflow that lists the scope itself", () => {
    expect(
      workflowsNeeding(catalogue, workflows, "mcp:read").map((w) => w.slug),
    ).toEqual(["maintenance", "review"]);
  });

  it("counts a workflow that lists a scope whose grant includes the denied one", () => {
    expect(
      workflowsNeeding(catalogue, workflows, "mcp:connect").map((w) => w.slug),
    ).toEqual(["maintenance", "review"]);
  });

  it("matches by name alone while the catalogue is empty", () => {
    const empty = scopeCatalogue(undefined);
    expect(
      workflowsNeeding(empty, workflows, "mcp:read").map((w) => w.slug),
    ).toEqual(["review"]);
    expect(workflowsNeeding(empty, workflows, "mcp:connect")).toEqual([]);
  });
});
