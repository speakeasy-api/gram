import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import { afterEach, describe, expect, it } from "vitest";
import {
  RolePermissionsSection,
  type ScopeGroup,
} from "./RolePermissionsSection";

function scope(
  slug: string,
  resourceType: ScopeDefinition["resourceType"],
): ScopeDefinition {
  return {
    slug: slug as ScopeDefinition["slug"],
    resourceType,
    description: `${slug} description`,
    agentEligible: true,
    visibility: "user_visible",
  };
}

const groups: ScopeGroup[] = [
  {
    label: "Build & Deploy",
    resourceType: "project",
    description: "Projects.",
    scopes: [scope("project:read", "project")],
  },
  {
    label: "MCP Servers",
    resourceType: "mcp",
    description: "MCP servers.",
    scopes: [
      scope("mcp:read", "mcp"),
      scope("mcp:write", "mcp"),
      scope("mcp:connect", "mcp"),
    ],
  },
];

function renderSection(selected: string[]) {
  render(
    <RolePermissionsSection
      groups={groups}
      selectedScopes={new Set(selected)}
      onToggleScope={() => {}}
      renderScopeRule={() => null}
    />,
  );
}

afterEach(cleanup);

describe("RolePermissionsSection tabs", () => {
  it("keeps only mcp:connect under MCP access", () => {
    renderSection(["mcp:read", "mcp:write", "mcp:connect", "project:read"]);

    expect(screen.getByRole("tab", { name: "MCP access (1)" })).toBeTruthy();
    expect(
      screen.getByRole("tab", { name: "Platform access (3)" }),
    ).toBeTruthy();
    expect(screen.getByText("mcp:connect")).toBeTruthy();
    expect(screen.queryByText("mcp:read")).toBeNull();
  });

  it("lists mcp:read and mcp:write under Platform access with the connect note", () => {
    renderSection(["mcp:read", "mcp:write", "mcp:connect", "project:read"]);

    fireEvent.mouseDown(screen.getByRole("tab", { name: /Platform access/ }));

    expect(screen.getByText("mcp:read")).toBeTruthy();
    expect(screen.getByText("mcp:write")).toBeTruthy();
    expect(screen.queryByText("mcp:connect")).toBeNull();
    expect(
      screen.getAllByText(/Also allows connecting to these servers/),
    ).toHaveLength(2);
  });

  it("labels the MCP tab with servers once counted, and no number before", () => {
    const { rerender } = render(
      <RolePermissionsSection
        groups={groups}
        selectedScopes={new Set(["mcp:connect"])}
        onToggleScope={() => {}}
        renderScopeRule={() => null}
        renderMcpAccess={() => null}
        mcpAccessCount={null}
      />,
    );
    expect(screen.getByRole("tab", { name: "MCP access" })).toBeTruthy();
    rerender(
      <RolePermissionsSection
        groups={groups}
        selectedScopes={new Set(["mcp:connect"])}
        onToggleScope={() => {}}
        renderScopeRule={() => null}
        renderMcpAccess={() => null}
        mcpAccessCount={4}
      />,
    );
    expect(screen.getByRole("tab", { name: "MCP access (4)" })).toBeTruthy();
  });
});
