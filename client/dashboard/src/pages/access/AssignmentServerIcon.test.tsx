import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  assetId: "logo-asset",
  isFetched: true,
  metadata: vi.fn(),
  catalog: vi.fn(),
}));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: (...args: unknown[]) => {
    state.metadata(...args);
    return {
      isFetched: state.isFetched,
      data: { metadata: { logoAssetId: state.assetId || undefined } },
    };
  },
}));
vi.mock("@/pages/catalog/useInstalledCatalogServer", () => ({
  useInstalledCatalogServer: (...args: unknown[]) => state.catalog(...args),
}));
import { ConfigProvider } from "@/components/ui/context/ConfigContext";
import { AssignmentServerIcon } from "./AssignmentServerIcon";
const server = {
  id: "server-a",
  name: "GitHub",
  projectId: "project-a",
  projectSlug: "engineering",
  slug: "github",
};
beforeEach(() => {
  state.assetId = "logo-asset";
  vi.clearAllMocks();
  state.isFetched = true;
  state.catalog.mockReturnValue(undefined);
  vi.stubGlobal(
    "Image",
    class extends EventTarget {
      complete = false;
      naturalWidth = 0;
      set src(_value: string) {
        queueMicrotask(() => {
          this.complete = true;
          this.naturalWidth = 32;
          this.dispatchEvent(new Event("load"));
        });
      }
    },
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
it("loads the configured logo from the server's own project", async () => {
  const { container } = render(<AssignmentServerIcon server={server} />);
  await waitFor(() =>
    expect(container.querySelector("img")?.getAttribute("src")).toContain(
      "/rpc/assets.serveImage?id=logo-asset",
    ),
  );
  expect(state.metadata).toHaveBeenCalledWith(
    { mcpServerId: "server-a", gramProject: "engineering" },
    undefined,
    expect.objectContaining({ retry: false, throwOnError: false }),
  );
});
it("uses the server's initial when branding is absent", () => {
  state.assetId = "";
  render(<AssignmentServerIcon server={server} />);
  expect(screen.getByText("G")).toBeDefined();
});

it("uses live curated variants from the server project, behind custom branding", async () => {
  state.catalog.mockReturnValue({
    registrySpecifier: "app.linear/linear",
    iconUrl: "legacy",
    meta: {
      "com.speakeasy.ai/icons": {
        default: { src: "light-logo" },
        dark: { src: "dark-logo" },
      },
    },
  });
  const view = (theme: "light" | "dark") => (
    <ConfigProvider theme={theme} setTheme={() => {}}>
      <AssignmentServerIcon server={server} />
    </ConfigProvider>
  );
  const { container, rerender } = render(view("dark"));
  await waitFor(() =>
    expect(container.querySelector("img")?.getAttribute("src")).toContain(
      "logo-asset",
    ),
  );
  expect(state.catalog).toHaveBeenCalledWith("server-a", false, "engineering");
  state.assetId = "";
  rerender(view("dark"));
  await waitFor(() =>
    expect(container.querySelector("img")?.getAttribute("src")).toBe(
      "dark-logo",
    ),
  );
  expect(container.querySelector("img")?.className).not.toContain("invert");
  expect(state.catalog).toHaveBeenCalledWith("server-a", true, "engineering");
  rerender(view("light"));
  await waitFor(() =>
    expect(container.querySelector("img")?.getAttribute("src")).toBe(
      "light-logo",
    ),
  );
});
it("retains the avatar initial when a curated image fails", async () => {
  state.assetId = "";
  state.catalog.mockReturnValue({
    registrySpecifier: "test",
    meta: { "com.speakeasy.ai/icons": { default: { src: "broken-curated" } } },
  });
  const failed = vi.fn();
  vi.stubGlobal(
    "Image",
    class extends EventTarget {
      set src(value: string) {
        queueMicrotask(() => {
          this.dispatchEvent(new Event("error"));
          failed(value);
        });
      }
    },
  );
  const { container } = render(<AssignmentServerIcon server={server} />);
  await waitFor(() => expect(failed).toHaveBeenCalledWith("broken-curated"));
  expect(screen.getByText("G")).toBeDefined();
  expect(container.querySelector("img")).toBeNull();
});

it("does not use cached catalog data before metadata settles", async () => {
  state.assetId = "";
  state.isFetched = false;
  state.catalog.mockReturnValue({
    registrySpecifier: "test",
    iconUrl: "cached",
  });
  const { container } = render(<AssignmentServerIcon server={server} />);
  expect(state.catalog).toHaveBeenCalledWith("server-a", false, "engineering");
  expect(container.querySelector("img")).toBeNull();
});
