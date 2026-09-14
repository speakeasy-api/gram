import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

// The footer's ThemeSwitcher needs a ConfigProvider; this suite is about the
// header, so stub it out rather than dragging in app-wide context.
vi.mock("./onboarding-footer", () => ({
  OnboardingFooter: () => null,
}));

import { SetupShell } from "./setup-shell";
import { SetupViewButton } from "./setup-view-button";

function renderAt(path: string, element: JSX.Element) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/:orgSlug/setup" element={element} />
        <Route path="/:orgSlug/setup/wizard" element={element} />
        <Route path="/:orgSlug/setup/:taskSlug" element={element} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(cleanup);

describe("SetupViewButton", () => {
  it("offers the wizard from the board", () => {
    renderAt("/org/setup", <SetupViewButton view="board" />);

    const link = screen.getByRole("link", { name: "Wizard" });
    expect(link.getAttribute("href")).toBe("/org/setup/wizard");
  });

  it("carries the open card into the wizard from its page", () => {
    renderAt("/org/setup/idp", <SetupViewButton view="board" />);

    expect(
      screen.getByRole("link", { name: "Wizard" }).getAttribute("href"),
    ).toBe("/org/setup/wizard?task=idp");
  });

  it("offers the board from the wizard", () => {
    renderAt("/org/setup/wizard", <SetupViewButton view="wizard" />);

    expect(
      screen.getByRole("link", { name: "Board" }).getAttribute("href"),
    ).toBe("/org/setup");
  });
});

describe("SetupShell", () => {
  it("puts the view button in the setup header", () => {
    renderAt(
      "/org/setup",
      <SetupShell view="board">
        <div>content</div>
      </SetupShell>,
    );

    expect(screen.getByRole("banner").textContent).toContain("Wizard");
    expect(screen.getByText("content")).toBeTruthy();
  });
});
