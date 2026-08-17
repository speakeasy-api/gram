import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import Skills from "./Skills";

const testState = vi.hoisted(() => ({
  projectId: "project_a",
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: testState.projectId }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    resourceId,
    scope,
  }: {
    children: ReactNode;
    resourceId?: string;
    scope: string;
  }) => (
    <div
      data-testid="scope-gate"
      data-resource-id={resourceId}
      data-scope={scope}
    >
      {children}
    </div>
  ),
}));

function renderPage(): void {
  render(
    <MemoryRouter initialEntries={["/"]}>
      <Routes>
        <Route path="/" element={<Skills />}>
          <Route index element={<div>Skills index route</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(cleanup);

describe("Skills", () => {
  it("renders the outlet behind the project-scoped Skills read gate", () => {
    renderPage();

    expect(screen.getByTestId("scope-gate").getAttribute("data-scope")).toBe(
      "skill:read",
    );
    expect(
      screen.getByTestId("scope-gate").getAttribute("data-resource-id"),
    ).toBe("project_a");
    expect(screen.getByText("Skills index route")).toBeTruthy();
  });
});
