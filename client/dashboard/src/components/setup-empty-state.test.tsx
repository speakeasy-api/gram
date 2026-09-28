import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import {
  CostsSetupEmptyState,
  RiskSetupEmptyState,
  ShadowAISetupEmptyState,
} from "./setup-empty-state";

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ deviceAgent: { href: () => "/org/device-agent" } }),
  useRoutes: () => ({
    policyCenter: { href: () => "/guardrails" },
    plugins: { href: () => "/plugins" },
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));

afterEach(cleanup);

it.each([
  {
    Component: RiskSetupEmptyState,
    action: "Create risk policy",
    href: "/guardrails",
  },
  {
    Component: CostsSetupEmptyState,
    action: "Set up OpenTelemetry",
    href: "/plugins",
  },
])(
  "offers $action for onboarding and filter guidance for narrowed results",
  ({ Component, action, href }) => {
    const { rerender } = render(
      <MemoryRouter>
        <Component />
      </MemoryRouter>,
    );
    expect(
      screen.getByRole("link", { name: action }).getAttribute("href"),
    ).toBe(href);

    rerender(
      <MemoryRouter>
        <Component filtered />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("link", { name: action })).toBeNull();
    expect(
      screen.getByText(/Widen the time range or clear filters/),
    ).toBeTruthy();
  },
);

it("links Shadow AI onboarding to device agent setup and keeps inventory-specific copy", () => {
  render(
    <MemoryRouter>
      <ShadowAISetupEmptyState
        heading="No MCP servers observed yet"
        description="Servers appear through agent traffic or access requests."
      />
    </MemoryRouter>,
  );
  expect(
    screen
      .getByRole("link", { name: "Set up device agent" })
      .getAttribute("href"),
  ).toBe("/org/device-agent");
  expect(
    screen.getByText(
      "Servers appear through agent traffic or access requests.",
    ),
  ).toBeTruthy();
});
