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

const check = (key: string, ready: boolean | null, optional = false) => ({
  key,
  label: key,
  description: key,
  ready,
  optional,
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
  expect(screen.getByText("1 step left")).toBeTruthy();
});

it("reads Ready when every required check is done", () => {
  renderChip([check("url", true), check("plugin", false, true)]);
  expect(screen.getByText("Ready")).toBeTruthy();
});
