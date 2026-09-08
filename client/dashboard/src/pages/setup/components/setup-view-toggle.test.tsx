import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
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
import { SetupViewToggle } from "./setup-view-toggle";

function LocationPath(): JSX.Element {
  const location = useLocation();
  return <output>{location.pathname}</output>;
}

function renderToggle(view: "board" | "wizard") {
  return render(
    <MemoryRouter initialEntries={["/org/setup"]}>
      <SetupViewToggle view={view} />
      <LocationPath />
    </MemoryRouter>,
  );
}

afterEach(cleanup);

describe("SetupViewToggle", () => {
  it("offers both setup views", () => {
    renderToggle("board");

    expect(screen.getByRole("button", { name: "Board" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Wizard" })).toBeTruthy();
  });

  it("marks the current view as active", () => {
    renderToggle("board");

    expect(
      screen
        .getByRole("button", { name: "Board" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
  });

  it("navigates to the wizard from the board", () => {
    renderToggle("board");

    fireEvent.click(screen.getByRole("button", { name: "Wizard" }));

    expect(screen.getByText("/org/setup/wizard")).toBeTruthy();
  });

  it("navigates back to the board from the wizard", () => {
    renderToggle("wizard");

    fireEvent.click(screen.getByRole("button", { name: "Board" }));

    expect(screen.getByText("/org/setup")).toBeTruthy();
  });
});

describe("SetupShell", () => {
  it("puts the view switcher in the setup header", () => {
    render(
      <MemoryRouter initialEntries={["/org/setup"]}>
        <SetupShell view="board">
          <div>content</div>
        </SetupShell>
      </MemoryRouter>,
    );

    expect(screen.getByRole("group", { name: "Setup view" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Wizard" })).toBeTruthy();
  });
});
