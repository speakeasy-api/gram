// @vitest-environment node
import { describe, expect, it } from "vitest";
import { scopeCatalogue, tasksNeeding } from "./scopeCatalogue";

import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { ScopeTask } from "@gram/client/models/components/scopetask.js";

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

function task(slug: string, scopes: string[]): ScopeTask {
  return { slug, label: slug, scopes: scopes as ScopeTask["scopes"] };
}

describe("tasksNeeding", () => {
  // The inclusions are a hand-written subset of the server's; its tests
  // guard the full graph.
  const catalogue = scopeCatalogue([
    def("mcp:connect"),
    def("mcp:read", ["mcp:connect"]),
    def("mcp:write", ["mcp:connect", "mcp:read"]),
    def("log:read"),
  ]);
  const tasks = [
    task("maintenance", ["mcp:write", "log:read"]),
    task("review", ["mcp:read", "log:read"]),
    task("unrelated", ["log:read"]),
  ];

  it("counts a task that lists the scope itself", () => {
    expect(
      tasksNeeding(catalogue, tasks, "mcp:read").map((w) => w.slug),
    ).toEqual(["maintenance", "review"]);
  });

  it("counts a task that lists a scope whose grant includes the denied one", () => {
    expect(
      tasksNeeding(catalogue, tasks, "mcp:connect").map((w) => w.slug),
    ).toEqual(["maintenance", "review"]);
  });

  it("matches by name alone while the catalogue is empty", () => {
    const empty = scopeCatalogue(undefined);
    expect(tasksNeeding(empty, tasks, "mcp:read").map((w) => w.slug)).toEqual([
      "review",
    ]);
    expect(tasksNeeding(empty, tasks, "mcp:connect")).toEqual([]);
  });
});
