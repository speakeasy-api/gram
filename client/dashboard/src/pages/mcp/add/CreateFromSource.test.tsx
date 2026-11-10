import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { toast } from "sonner";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import CreateFromSource from "./CreateFromSource";
const api = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
}));
vi.mock("@/components/page-templates", () => ({
  FormPage: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    toolsets: { create: api.create, updateBySlug: api.update },
  }),
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));
vi.mock("@/components/side-panel/side-panel-context", () => ({
  useSidePanel: () => ({ openPanel: vi.fn() }),
}));
vi.mock("@/components/icon-dither", () => ({
  useIconDither: () => ({ canvasRef: null, start: vi.fn(), stop: vi.fn() }),
}));
vi.mock("@/components/sources/source-list", () => ({
  useProjectSources: () => ({
    sources: [
      {
        key: "first",
        name: "First source",
        kind: "openapi",
        documentId: "first",
      },
      {
        key: "second",
        name: "Second source",
        kind: "openapi",
        documentId: "second",
      },
    ],
  }),
  sourceAssetId: () => "asset",
}));
vi.mock("@/hooks/toolTypes", () => ({
  useListTools: () => ({
    data: {
      tools: [
        { type: "http", openapiv3DocumentId: "first", toolUrn: "urn:first" },
        { type: "http", openapiv3DocumentId: "second", toolUrn: "urn:second" },
      ],
    },
  }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));
beforeEach(() => {
  vi.resetAllMocks();
  api.create.mockResolvedValue({
    id: "toolset",
    slug: "toolset",
    name: "First source",
  });
  api.update.mockResolvedValue(undefined);
});
afterEach(cleanup);
it("creates a server seeded with only the chosen source's tools", async () => {
  render(
    <MemoryRouter>
      <CreateFromSource />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: /First source/ }));
  fireEvent.click(screen.getByRole("button", { name: "Create" }));
  await waitFor(() =>
    expect(api.update).toHaveBeenCalledWith({
      slug: "toolset",
      updateToolsetRequestBody: { toolUrns: ["urn:first"] },
    }),
  );
  expect(api.create).toHaveBeenCalledOnce();
});
it("shows the seeding error when filling the server fails", async () => {
  api.update.mockRejectedValueOnce(new Error("Seeding failed"));
  render(
    <MemoryRouter>
      <CreateFromSource />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: /First source/ }));
  fireEvent.click(screen.getByRole("button", { name: "Create" }));
  expect(await screen.findByText("Seeding failed")).toBeTruthy();
  expect(api.create).toHaveBeenCalledOnce();
  expect(toast.success).not.toHaveBeenCalled();
});
