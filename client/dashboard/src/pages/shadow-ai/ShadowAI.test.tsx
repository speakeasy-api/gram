import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  tab: "mcp-servers" as string,
  slot: {
    pending: false,
    failed: false,
    existing: undefined as unknown,
    href: undefined as string | undefined,
  },
}));

vi.mock("nuqs", () => ({
  parseAsStringLiteral: () => ({ withDefault: () => undefined }),
  useQueryState: () => [state.tab, vi.fn()],
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/components/page-templates", () => ({
  TabbedPage: ({
    title,
    primaryAction,
    tabs,
    children,
  }: {
    title: string;
    primaryAction?: ReactNode;
    tabs: { value: string; label: string; href: string }[];
    children: ReactNode;
  }) => (
    <div>
      <h1>{title}</h1>
      <nav aria-label="Tabs">
        {tabs.map((tab) => (
          <a key={tab.value} href={tab.href}>
            {tab.label}
          </a>
        ))}
      </nav>
      <div data-testid="primary-action">{primaryAction}</div>
      {children}
    </div>
  ),
}));

vi.mock("@/pages/security/MCPApprovalsTab", () => ({
  MCPApprovalsTab: () => <div>MCP servers body</div>,
}));

vi.mock("@/pages/security/ShadowAITargetsTab", () => ({
  ShadowAITargetsTab: () => <div>AI tools body</div>,
}));

vi.mock("@/pages/security/use-shadow-ai-policy-slot", () => ({
  useShadowAIPolicySlot: () => state.slot,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    policyCenter: { new: { href: () => "/org/projects/p/risk-policies/new" } },
    shadowAI: { href: () => "/org/projects/p/shadow-ai" },
  }),
}));

import ShadowAI from "./ShadowAI";

function renderPage(): void {
  render(
    <MemoryRouter>
      <ShadowAI />
    </MemoryRouter>,
  );
}

afterEach(cleanup);

beforeEach(() => {
  state.tab = "mcp-servers";
  state.slot = {
    pending: false,
    failed: false,
    existing: undefined,
    href: undefined,
  };
});

describe("ShadowAI", () => {
  it("shows the MCP servers and AI tools tabs, MCP servers first", () => {
    renderPage();

    const tabs = screen
      .getAllByRole("link")
      .filter((link) => link.getAttribute("href")?.startsWith("?tab="));
    expect(
      tabs.map((tab) => [tab.textContent, tab.getAttribute("href")]),
    ).toEqual([
      ["MCP servers", "?tab=mcp-servers"],
      ["AI tools", "?tab=ai-tools"],
    ]);
    expect(screen.getByText("MCP servers body")).toBeTruthy();
    expect(screen.queryByText("AI tools body")).toBeNull();
  });

  it("renders the AI tools tab when it is selected", () => {
    state.tab = "ai-tools";
    renderPage();

    expect(screen.getByText("AI tools body")).toBeTruthy();
    expect(screen.queryByText("MCP servers body")).toBeNull();
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
