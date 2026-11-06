import { render, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { ConfigProvider } from "@/components/ui/context/ConfigContext";
import { ServerCard } from "./ServerCard";
import { ServerTableRow } from "./ServerTableRow";
import type { CatalogServer } from "./hooks";

vi.mock("@/components/icon-dither", () => ({
  useIconDither: () => ({ canvasRef: null, start: () => {}, stop: () => {} }),
}));
vi.mock("@/components/ui/Card", () => ({
  Card: {
    Entity: ({ icon }: { icon?: ReactNode }) => (
      <div data-testid="tile" data-has-icon={!!icon}>
        {icon}
      </div>
    ),
  },
}));
vi.mock("@/components/ui/DotRow", () => ({
  DotRow: ({ icon }: { icon?: ReactNode }) => (
    <div data-testid="tile" data-has-icon={!!icon}>
      {icon}
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
        <ConfigProvider theme={theme} setTheme={() => {}}>
          <Component
            server={server}
            detailHref="/catalog/test"
            installState="unknown"
            onAdd={() => {}}
          />
        </ConfigProvider>
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
