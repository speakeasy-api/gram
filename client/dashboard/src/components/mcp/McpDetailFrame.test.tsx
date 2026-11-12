import type { ReadinessCheck } from "@/components/mcp-server-readiness-bar";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { McpDetailFrame } from "./McpDetailFrame";
import type { McpDetailChrome } from "./mcpDetailChrome";

vi.mock("@/components/page-layout", () => {
  const Pass = ({ children }: { children?: React.ReactNode }) => (
    <>{children}</>
  );
  const Header = Object.assign(Pass, { Breadcrumbs: () => null });
  return { Page: Object.assign(Pass, { Header, Body: Pass }) };
});

afterEach(cleanup);

function renderChip(checks: ReadinessCheck[]) {
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

it("names what is broken before any step left", () => {
  renderChip([check("url", false), check("tunnel", false, false, true)]);
  expect(screen.getByText("1 issue: tunnel")).toBeTruthy();
  expect(screen.queryByText(/step/)).toBeNull();
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
