import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  scope: undefined as unknown,
}));

vi.mock("@/components/page-templates", () => ({
  ResourceListPage: ({
    title,
    scope,
    primaryAction,
    children,
  }: {
    title: string;
    scope: unknown;
    primaryAction?: ReactNode;
    children: ReactNode;
  }) => {
    state.scope = scope;
    return (
      <div>
        <h1>{title}</h1>
        <div data-testid="primary-action">{primaryAction}</div>
        {children}
      </div>
    );
  },
}));

vi.mock("@/pages/security/MCPApprovalsTab", () => ({
  MCPApprovalsTab: () => <div>MCP server inventory</div>,
}));

vi.mock("@/components/shadow-mcp/ShadowMCPPolicySummary", () => ({
  ShadowMCPPolicySummary: () => <div>policy summary</div>,
}));

import ShadowMCP from "./ShadowMCP";

afterEach(cleanup);

beforeEach(() => {
  state.scope = undefined;
});

describe("ShadowMCP", () => {
  it("shows the MCP server inventory to org admins", () => {
    render(
      <MemoryRouter>
        <ShadowMCP />
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { name: "Shadow MCP" })).toBeTruthy();
    expect(screen.getByText("MCP server inventory")).toBeTruthy();
    expect(state.scope).toBe("org:admin");
  });

  it("states the policy above the inventory instead of a header toggle", () => {
    render(
      <MemoryRouter>
        <ShadowMCP />
      </MemoryRouter>,
    );

    expect(screen.getByText("policy summary")).toBeTruthy();
    expect(screen.getByTestId("primary-action").textContent).toBe("");
    expect(screen.queryByRole("link", { name: "Set up policy" })).toBeNull();
  });
});
