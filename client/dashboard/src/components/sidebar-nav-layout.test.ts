// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AppRoute } from "@/routes";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ScopeGatedNavEntry } from "./scope-gated-nav-group";
import { sidebarNavLayout } from "./sidebar-nav-layout";

const entry = (title: string, scope: Scope): ScopeGatedNavEntry => ({
  item: { title, url: `/${title}` } as AppRoute,
  scope,
});

const groups = [
  { label: "Identity", items: [entry("People", "project:read")] },
  {
    label: "MCP Gateway",
    items: [
      entry("MCP", "mcp:read"),
      entry("Environments", "project:write"),
      entry("Playground", "mcp:connect"),
    ],
  },
  { label: "Security and Policy", items: [entry("Risk", "org:admin")] },
];

const holding =
  (...held: Scope[]) =>
  (scopes: Scope[]): boolean =>
    scopes.some((scope) => held.includes(scope));

describe("sidebarNavLayout", () => {
  it("lists a lone group's visible items flat", () => {
    const layout = sidebarNavLayout(
      groups,
      false,
      holding("mcp:read", "mcp:connect"),
    );
    expect(layout.groups.map((g) => g.label)).toEqual(["MCP Gateway"]);
    expect(layout.flatEntries?.map((e) => e.item.title)).toEqual([
      "MCP",
      "Playground",
    ]);
  });

  it("keeps a lone group with staged items as a group", () => {
    const staged = [
      {
        label: "Observability",
        items: [
          entry("Costs", "project:read"),
          {
            item: {
              title: "Explore",
              url: "/explore",
              stage: "preview",
            } as AppRoute,
            scope: "project:read" as Scope,
          },
        ],
      },
    ];
    const layout = sidebarNavLayout(staged, false, holding("project:read"));
    expect(layout.groups.map((g) => g.label)).toEqual(["Observability"]);
    expect(layout.flatEntries).toBeUndefined();
  });

  it("drops the divider when nothing sits above the groups", () => {
    expect(
      sidebarNavLayout(groups, false, holding("mcp:read")).showDivider,
    ).toBe(false);
  });

  it("keeps the divider when items sit on both sides", () => {
    expect(
      sidebarNavLayout(groups, true, holding("mcp:read")).showDivider,
    ).toBe(true);
  });

  it("drops the divider when no group is visible", () => {
    const layout = sidebarNavLayout(groups, true, holding());
    expect(layout.groups).toEqual([]);
    expect(layout.flatEntries).toBeUndefined();
    expect(layout.showDivider).toBe(false);
  });

  it("checks each entry against its own resource and project", () => {
    const scoped: ScopeGatedNavEntry = {
      ...entry("Skills", "skill:read"),
      resourceId: "res_a",
      projectId: "proj_a",
    };
    const layout = sidebarNavLayout(
      [{ label: "MCP Gateway", items: [scoped, entry("MCP", "mcp:read")] }],
      false,
      (_scopes, resourceId, projectId) =>
        resourceId === "res_a" && projectId === "proj_a",
    );
    expect(layout.flatEntries?.map((e) => e.item.title)).toEqual(["Skills"]);
  });

  it("keeps groups collapsible when more than one is visible", () => {
    const layout = sidebarNavLayout(
      groups,
      true,
      holding("project:read", "mcp:read", "org:admin"),
    );
    expect(layout.groups.map((g) => g.label)).toEqual([
      "Identity",
      "MCP Gateway",
      "Security and Policy",
    ]);
    expect(layout.flatEntries).toBeUndefined();
  });
});
