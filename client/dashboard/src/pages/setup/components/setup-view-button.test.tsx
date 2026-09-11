import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { SetupViewButton } from "./setup-view-button";

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setup: { href: () => "/org/setup" },
    setupWizard: { href: () => "/org/setup/wizard" },
  }),
}));
afterEach(cleanup);

it("does not expose navigation while a task write is settling", () => {
  render(
    <MemoryRouter>
      <SetupViewButton wizard disabled />
    </MemoryRouter>,
  );
  expect(screen.queryByRole("link")).toBeNull();
  expect(
    screen
      .getByRole("button", { name: "Workstreams" })
      .hasAttribute("disabled"),
  ).toBe(true);
});

function View() {
  const location = useLocation();
  return <SetupViewButton wizard={location.pathname.endsWith("/wizard")} />;
}

it("switches both ways preserving task, section, project and hash without a board mode", () => {
  render(
    <MemoryRouter
      initialEntries={[
        "/org/setup?task=anthropic-observability&step=confirm-traffic&projectSlug=selected&view=kanban#details",
      ]}
    >
      <View />
    </MemoryRouter>,
  );
  const wizard = screen.getByRole("link", { name: "Wizard" });
  expect(wizard.getAttribute("href")).toBe(
    "/org/setup/wizard?task=anthropic-observability&step=confirm-traffic&projectSlug=selected#details",
  );
  fireEvent.click(wizard);
  const workstreams = screen.getByRole("link", { name: "Workstreams" });
  expect(workstreams.getAttribute("href")).toBe(
    "/org/setup?task=anthropic-observability&step=confirm-traffic&projectSlug=selected#details",
  );
  fireEvent.click(workstreams);
  expect(screen.getByRole("link", { name: "Wizard" })).toBeTruthy();
});
