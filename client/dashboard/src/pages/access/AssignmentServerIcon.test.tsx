import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({ assetId: "logo-asset", metadata: vi.fn() }));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: (...args: unknown[]) => {
    state.metadata(...args);
    return { data: { metadata: { logoAssetId: state.assetId || undefined } } };
  },
}));
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
