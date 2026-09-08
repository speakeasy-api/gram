import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import { HeadlessContent } from "./HeadlessContent";
import { MemoryRouter } from "react-router";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));
vi.mock("@/components/mode-switch-starfield", () => ({
  ModeSwitchStarfield: () => null,
}));
vi.mock("@/components/gram-logo/variants/icon", () => ({
  GramIcon: () => null,
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ home: { goTo: vi.fn() } }),
}));
// The wizard is a stack of sheets over live onboarding queries; this page's
// own job is the list that opens it.
vi.mock("./PlatformMCP", () => ({
  PlatformMCPOnboardingContent: () => null,
}));

afterEach(cleanup);

// The agent list is the page's only <ul>; the numbered steps below it are an
// <ol>, so this stays scoped to the rows under test.
const agentNames = (container: HTMLElement) =>
  Array.from(container.querySelectorAll("ul li")).map(
    (item) => item.querySelector("span > span")?.textContent,
  );

describe("HeadlessContent", () => {
  it("describes the control-plane workflows available to the agent", () => {
    render(
      <MemoryRouter>
        <HeadlessContent />
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("heading", { name: "Connect your agent" }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Administer the control plane from the agent you already work in. Deploy and Manage MCP gateway servers, Review security policies and deep dive your AI usage data.",
      ),
    ).toBeTruthy();
  });

  it("offers a catch-all agent last", () => {
    const { container } = render(
      <MemoryRouter>
        <HeadlessContent />
      </MemoryRouter>,
    );

    const names = agentNames(container);
    expect(names).toContain("Other agent");
    expect(names.at(-1)).toBe("Other agent");
    expect(screen.getByText("Any MCP-capable agent")).toBeTruthy();
  });

  it("still lists the certified agents ahead of it", () => {
    const { container } = render(
      <MemoryRouter>
        <HeadlessContent />
      </MemoryRouter>,
    );

    expect(agentNames(container).slice(0, -1)).toEqual([
      "Claude Code",
      "Claude Cowork",
      "OpenAI Codex",
      "Cursor",
      "opencode",
    ]);
  });
});
