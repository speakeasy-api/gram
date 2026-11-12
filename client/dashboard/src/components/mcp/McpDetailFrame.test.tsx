import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { McpDetailFrame } from "./McpDetailFrame";
import type { McpDetailChrome, McpReadinessCheck } from "./mcpDetailChrome";

vi.mock("@/components/page-layout", () => {
  const Pass = ({ children }: { children?: React.ReactNode }) => (
    <>{children}</>
  );
  const Header = Object.assign(Pass, { Breadcrumbs: () => null });
  return { Page: Object.assign(Pass, { Header, Body: Pass }) };
});

afterEach(cleanup);

function renderChip(checks: McpReadinessCheck[]) {
  const chrome: McpDetailChrome = {
    name: "Example",
    icon: () => null,
    kindLabel: undefined,
    statusControl: null,
    primaryUrl: undefined,
    readinessChecks: checks,
    installLinks: [],
    items: [],
  };
  render(
    <MemoryRouter>
      <McpDetailFrame
        chrome={chrome}
        breadcrumbs={{ substitutions: {}, skipSegments: [] }}
      >
        {null}
      </McpDetailFrame>
    </MemoryRouter>,
  );
}

const check = (
  key: string,
  ready: boolean | null,
  optional = false,
  broken = false,
) => ({
  key,
  label: key,
  description: key,
  ready,
  optional,
  broken,
});

it("says Checking… rather than a step left while a check is undetermined", () => {
  renderChip([check("url", true), check("auth", null)]);
  expect(screen.getByText("Checking…")).toBeTruthy();
  expect(screen.queryByText(/step/)).toBeNull();
});

it("counts only known-incomplete required checks as steps", () => {
  renderChip([
    check("url", false),
    check("auth", null),
    check("plugin", false, true),
  ]);
  // The chip names the one step left, not just a count.
  expect(screen.getByText("1 step left: url")).toBeTruthy();
});

it("lists every step left in the chip's accessible name", () => {
  renderChip([check("url", false), check("auth", false)]);
  expect(screen.getByText("2 steps left")).toBeTruthy();
  expect(
    screen.getByRole("button", {
      name: "2 steps left: url, auth. Open checklist",
    }),
  ).toBeTruthy();
});

it("reads Ready when every required check is done", () => {
  renderChip([check("url", true), check("plugin", false, true)]);
  expect(screen.getByText("Ready")).toBeTruthy();
});

it("counts issues and steps together, so the chip matches the list", () => {
  renderChip([check("url", false), check("tunnel", false, false, true)]);
  expect(
    screen.getByRole("button", {
      name: "2 to fix: url, tunnel. Open checklist",
    }),
  ).toBeTruthy();
});

it("uses a broken check's short label when it is the only one", () => {
  renderChip([
    check("url", true),
    { ...check("availability", false, false, true), chipLabel: "Turned off" },
  ]);
  expect(screen.getByText("Turned off")).toBeTruthy();
});

it("names no step while another check is still pending", () => {
  // Whichever query settles first, the chip says the same thing.
  renderChip([check("url", false), { ...check("auth", null), pending: true }]);
  expect(screen.getByText("Checking…")).toBeTruthy();
  expect(screen.queryByText(/step/)).toBeNull();
});

it("labels each open row in words and closes when a row is followed", () => {
  renderChip([
    { ...check("url", false), href: "/settings#server-url" },
    { ...check("plugin", false, true), href: "/plugins" },
  ]);
  fireEvent.click(screen.getByText("1 step left: url"));
  expect(screen.getByText("To do")).toBeTruthy();
  expect(screen.getByText("Optional")).toBeTruthy();
  fireEvent.click(screen.getByRole("link", { name: /url/ }));
  expect(screen.queryByText("To do")).toBeNull();
});

it("counts several broken checks as issues", () => {
  renderChip([
    check("availability", false, false, true),
    check("tunnel", false, false, true),
  ]);
  expect(
    screen.getByRole("button", {
      name: "2 issues: availability, tunnel. Open checklist",
    }),
  ).toBeTruthy();
});

it("is not Ready while a broken check is still undetermined", () => {
  renderChip([check("url", true), check("tunnel", null, false, true)]);
  expect(screen.getByText("Checking…")).toBeTruthy();
});
