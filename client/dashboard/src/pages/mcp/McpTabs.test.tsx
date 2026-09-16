import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { McpTabs } from "./McpTabs";

const projectPath = "/org/projects/proj";

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: Object.assign(
      { href: () => `${projectPath}/mcp` },
      {
        catalog: { href: () => `${projectPath}/mcp/catalog` },
        sources: { href: () => `${projectPath}/mcp/sources` },
        deployments: { href: () => `${projectPath}/mcp/deployments` },
      },
    ),
  }),
}));

afterEach(cleanup);

describe("McpTabs", () => {
  it("offers the catalog alongside the other MCP views", () => {
    render(
      <MemoryRouter>
        <McpTabs active="servers" />
      </MemoryRouter>,
    );

    expect(screen.getAllByRole("tab").map((tab) => tab.textContent)).toEqual([
      "MCP Servers",
      "Catalog",
      "Sources",
      "Deployments",
    ]);
    expect(
      screen.getByRole("tab", { name: "Catalog" }).getAttribute("href"),
    ).toBe(`${projectPath}/mcp/catalog`);
  });

  it("marks the catalog tab as the selected one on the catalog page", () => {
    render(
      <MemoryRouter>
        <McpTabs active="catalog" />
      </MemoryRouter>,
    );

    expect(
      screen
        .getByRole("tab", { name: "Catalog" })
        .getAttribute("aria-selected"),
    ).toBe("true");
  });
});
