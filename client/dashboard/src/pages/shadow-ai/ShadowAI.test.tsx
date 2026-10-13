import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ShadowAIIndexRedirect, ShadowAISection } from "./ShadowAI";

const mocks = vi.hoisted(() => ({
  flag: { status: "enabled" } as { status: string },
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flag as FeatureFlagResult,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    policyCenter: { href: () => "/guardrails" },
    shadowAI: {
      harnesses: { href: () => "/shadow-ai/harnesses" },
      assistants: { href: () => "/shadow-ai/assistants" },
      models: { href: () => "/shadow-ai/models" },
      mcps: { href: () => "/shadow-ai/mcps" },
    },
  }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/components/page-layout", () => ({
  Page: Object.assign(({ children }: { children?: ReactNode }) => children, {
    Header: Object.assign(
      ({ children }: { children?: ReactNode }) => children,
      { Breadcrumbs: () => null },
    ),
    Body: ({ children }: { children?: ReactNode }) => children,
  }),
}));

function LocationPath(): JSX.Element {
  const location = useLocation();
  return <output data-testid="location">{location.pathname}</output>;
}

function renderAt(url: string, element: ReactNode) {
  return render(
    <MemoryRouter initialEntries={[url]}>
      {element}
      <LocationPath />
    </MemoryRouter>,
  );
}

describe("Shadow AI section under the guardrails flag", () => {
  afterEach(() => {
    cleanup();
    mocks.flag = { status: "enabled" };
  });

  it("sends a tab page to Guardrails once folded in", () => {
    renderAt(
      "/shadow-ai/harnesses",
      <ShadowAISection activeTab="harnesses">
        <p>harness rows</p>
      </ShadowAISection>,
    );

    expect(screen.getByTestId("location").textContent).toBe("/guardrails");
    expect(screen.queryByText("harness rows")).toBeNull();
  });

  it("sends the section index to Guardrails once folded in", () => {
    renderAt("/shadow-ai", <ShadowAIIndexRedirect />);

    expect(screen.getByTestId("location").textContent).toBe("/guardrails");
  });

  it("keeps the tabs and the harnesses landing while the flag is off", () => {
    mocks.flag = { status: "disabled" };
    renderAt(
      "/shadow-ai/harnesses",
      <ShadowAISection activeTab="harnesses">
        <p>harness rows</p>
      </ShadowAISection>,
    );
    expect(screen.getByText("harness rows")).toBeTruthy();
    expect(screen.getByText("Local Models")).toBeTruthy();

    cleanup();
    renderAt("/shadow-ai", <ShadowAIIndexRedirect />);
    expect(screen.getByTestId("location").textContent).toBe(
      "/shadow-ai/harnesses",
    );
  });
});
