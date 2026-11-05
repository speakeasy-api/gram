import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ConfigProvider } from "@/components/ui/context/ConfigContext";
import { CatalogLogo } from "./CatalogLogo";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
const mocks = vi.hoisted(() => ({
  isFetched: true,
  metadata: undefined as undefined | { logoAssetId: string },
  catalog: {
    registrySpecifier: "app.linear/linear",
    iconUrl: "legacy",
    meta: {
      "com.speakeasy.ai/icons": {
        default: { src: "default" },
        dark: { src: "dark" },
      },
    },
  },
}));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: () => ({
    isFetched: mocks.isFetched,
    data: { metadata: mocks.metadata },
  }),
}));
vi.mock("./useInstalledCatalogServer", () => ({
  useInstalledCatalogServer: () => mocks.catalog,
}));
vi.mock("@/components/asset-image", () => ({
  AssetImage: ({ assetId }: { assetId: string }) => (
    <img alt="custom" src={assetId} />
  ),
}));
describe("catalog branding", () => {
  it("reacts to active theme and live catalog updates without inversion", () => {
    const view = (theme: "light" | "dark") => (
      <ConfigProvider theme={theme} setTheme={() => {}}>
        <SourceMcpIcon mcpServerId="server" className="size-8" />
      </ConfigProvider>
    );
    const { rerender, container } = render(view("light"));
    expect(container.querySelector("img")!.getAttribute("src")).toBe("default");
    rerender(view("dark"));
    expect(container.querySelector("img")!.getAttribute("src")).toBe("dark");
    expect(container.querySelector("img")!.className).not.toContain("invert");
    mocks.catalog.meta["com.speakeasy.ai/icons"].dark.src = "updated";
    rerender(view("dark"));
    expect(container.querySelector("img")!.getAttribute("src")).toBe("updated");
    mocks.metadata = { logoAssetId: "custom-asset" };
    rerender(view("dark"));
    expect(container.querySelector("img")!.getAttribute("src")).toBe(
      "custom-asset",
    );
    mocks.metadata = undefined;
  });
  it("preserves placeholder for dark-only logos in light theme", () => {
    render(
      <CatalogLogo
        server={{
          registrySpecifier: "test",
          meta: { "com.speakeasy.ai/icons": { dark: { src: "dark" } } },
        }}
        fallback={<span>placeholder</span>}
      />,
    );
    expect(screen.getByText("placeholder")).toBeTruthy();
  });
});

it("does not render cached curated branding while metadata is pending", () => {
  mocks.metadata = undefined;
  mocks.isFetched = false;
  const { container } = render(
    <SourceMcpIcon mcpServerId="server" className="size-8" />,
  );
  expect(container.querySelector("img")).toBeNull();
  mocks.isFetched = true;
});
