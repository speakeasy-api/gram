import { render, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { ConfigProvider } from "@/components/ui/context/ConfigContext";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ServerCard } from "./ServerCard";
import { ServerTableRow } from "./ServerTableRow";
import type { CatalogServer } from "./hooks";

vi.mock("@/components/icon-dither", () => ({
  useIconDither: () => ({ canvasRef: null, start: () => {}, stop: () => {} }),
}));
vi.mock("@/components/ui/Card", () => ({
  Card: {
    Entity: ({
      icon,
      children,
    }: {
      icon?: ReactNode;
      children?: ReactNode;
    }) => (
      <div data-testid="tile" data-has-icon={!!icon}>
        {icon}
        {children}
      </div>
    ),
  },
}));
vi.mock("@/components/ui/DotRow", () => ({
  DotRow: ({ icon, children }: { icon?: ReactNode; children?: ReactNode }) => (
    <div data-testid="tile" data-has-icon={!!icon}>
      {icon}
      <table>
        <tbody>
          <tr>{children}</tr>
        </tbody>
      </table>
    </div>
  ),
}));
const server = {
  registrySpecifier: "test/server",
  version: "1",
  description: "Example server",
  supportsDcr: false,
  meta: { "com.speakeasy.ai/icons": { dark: { src: "dark-logo" } } },
} as CatalogServer;
afterEach(cleanup);
describe.each([
  ["card", ServerCard],
  ["row", ServerTableRow],
] as const)("catalog tile %s", (_name, Component) => {
  it("uses the no-icon presentation for dark-only assets in light mode", () => {
    const view = (theme: "light" | "dark") => (
      <MemoryRouter>
        <TooltipProvider>
          <ConfigProvider theme={theme} setTheme={() => {}}>
            <Component
              server={server}
              detailHref="/catalog/test"
              installState="unknown"
              onAdd={() => {}}
            />
          </ConfigProvider>
        </TooltipProvider>
      </MemoryRouter>
    );
    const { getByTestId, rerender, container } = render(view("light"));
    expect(getByTestId("tile").getAttribute("data-has-icon")).toBe("false");
    rerender(view("dark"));
    expect(getByTestId("tile").getAttribute("data-has-icon")).toBe("true");
    expect(container.querySelector("img")?.getAttribute("src")).toBe(
      "dark-logo",
    );
  });
});

describe.each([
  ["card", ServerCard],
  ["row", ServerTableRow],
] as const)("catalog tile %s team usage", (_name, Component) => {
  const view = (teamUsage?: { userCount: number; callCount: number }) => (
    <MemoryRouter>
      <TooltipProvider>
        <ConfigProvider theme="light" setTheme={() => {}}>
          <Component
            server={server}
            detailHref="/catalog/test"
            installState="unknown"
            onAdd={() => {}}
            showTeamUsage
            teamUsage={teamUsage}
          />
        </ConfigProvider>
      </TooltipProvider>
    </MemoryRouter>
  );

  it("shows the team's usage once it arrives", () => {
    const { queryByText, rerender } = render(view());
    expect(queryByText(/Used by/)).toBeNull();
    rerender(view({ userCount: 5, callCount: 51 }));
    expect(queryByText("Used by 5 people · 51 calls")).not.toBeNull();
  });
});
