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

function LocationProbe() {
  const location = useLocation();
  return <output>{location.pathname + location.search + location.hash}</output>;
}

it("returns from a board card without reopening its wizard and consumes only board context", () => {
  render(
    <MemoryRouter
      initialEntries={[
        "/org/setup/wizard?task=litellm&step=connect&projectSlug=selected&filter=mine&from=workstreams#details",
      ]}
    >
      <View />
      <LocationProbe />
    </MemoryRouter>,
  );
  const back = screen.getByRole("link", { name: "Workstreams" });
  expect(back.querySelector("svg.lucide-arrow-left")).not.toBeNull();
  expect(back.getAttribute("href")).toBe(
    "/org/setup?projectSlug=selected&filter=mine#details",
  );
  fireEvent.click(back);
  expect(screen.getByRole("status").textContent).toBe(
    "/org/setup?projectSlug=selected&filter=mine#details",
  );
  fireEvent.click(screen.getByRole("link", { name: "Wizard" }));
  expect(
    screen
      .getByRole("link", { name: "Workstreams" })
      .querySelector("svg.lucide-arrow-left"),
  ).toBeNull();
});

it.each(["", "?from=other&task=litellm&step=connect"])(
  "preserves ordinary wizard navigation without board provenance: %s",
  (search) => {
    render(
      <MemoryRouter initialEntries={[`/org/setup/wizard${search}`]}>
        <View />
      </MemoryRouter>,
    );
    const link = screen.getByRole("link", { name: "Workstreams" });
    expect(link.querySelector("svg.lucide-arrow-left")).toBeNull();
    expect(link.getAttribute("href")).toBe(`/org/setup${search}`);
  },
);

it.each([false, true])(
  "uses shared edge alignment when disabled is %s",
  (disabled) => {
    render(
      <MemoryRouter initialEntries={["/org/setup/wizard?from=workstreams"]}>
        <SetupViewButton wizard disabled={disabled} edge="start" />
      </MemoryRouter>,
    );
    const control = screen.getByRole(disabled ? "button" : "link", {
      name: "Workstreams",
    });
    expect(control.classList.contains("px-3")).toBe(true);
    expect(control.classList.contains("-ms-3")).toBe(true);
    expect(control.querySelector("svg.lucide-arrow-left")).not.toBeNull();
  },
);

it("preserves default header padding without an override", () => {
  render(
    <MemoryRouter>
      <SetupViewButton wizard />
    </MemoryRouter>,
  );
  const control = screen.getByRole("link", { name: "Workstreams" });
  expect(control.classList.contains("px-3")).toBe(true);
  expect(control.classList.contains("pl-0")).toBe(false);
});
