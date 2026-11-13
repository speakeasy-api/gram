import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  slot: {
    pending: false,
    failed: false,
    existing: undefined as unknown,
    href: undefined as string | undefined,
  },
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

vi.mock("@/pages/security/use-shadow-ai-policy-slot", () => ({
  useShadowAIPolicySlot: () => state.slot,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    policyCenter: { new: { href: () => "/org/projects/p/risk-policies/new" } },
  }),
}));

import ShadowMCP from "./ShadowMCP";

function renderPage(): void {
  render(
    <MemoryRouter>
      <ShadowMCP />
    </MemoryRouter>,
  );
}

afterEach(cleanup);

beforeEach(() => {
  state.slot = {
    pending: false,
    failed: false,
    existing: undefined,
    href: undefined,
  };
  state.scope = undefined;
});

describe("ShadowMCP", () => {
  it("shows the MCP server inventory to org admins", () => {
    renderPage();

    expect(screen.getByRole("heading", { name: "Shadow MCP" })).toBeTruthy();
    expect(screen.getByText("MCP server inventory")).toBeTruthy();
    expect(state.scope).toBe("org:admin");
  });

  it("offers to set up the policy when the project has none", () => {
    renderPage();

    expect(
      screen.getByRole("link", { name: "Set up policy" }).getAttribute("href"),
    ).toBe("/org/projects/p/risk-policies/new?kind=shadow-ai");
  });

  it("opens the existing policy when there is one", () => {
    state.slot = {
      pending: false,
      failed: false,
      existing: { id: "policy-1" },
      href: "/org/projects/p/risk-policies/policy-1",
    };
    renderPage();

    expect(
      screen.getByRole("link", { name: "Edit policy" }).getAttribute("href"),
    ).toBe("/org/projects/p/risk-policies/policy-1");
    expect(screen.queryByRole("link", { name: "Set up policy" })).toBeNull();
  });

  // A pending or failed read is not proof the slot is free, so the header
  // offers nothing rather than a create that the server would refuse.
  it.each([
    { pending: true, failed: false },
    { pending: false, failed: true },
  ])("offers no policy action while the slot is unknown (%o)", (slot) => {
    state.slot = { ...state.slot, ...slot };
    renderPage();

    expect(screen.getByTestId("primary-action").textContent).toBe("");
  });
});
