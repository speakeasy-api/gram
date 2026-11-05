import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";

import { renderWithApp } from "@/test/harness";
import { AdminMcpTools, type AdminMcpTool } from "./AdminMcpTools";

const fetchTools = vi.hoisted(() => vi.fn());
vi.mock("@/lib/gramAdminApi", () => ({ gramAdminFetch: fetchTools }));

const tools: AdminMcpTool[] = [
  {
    name: "list_registry_promotion_candidates",
    title: "List Catalogue Promotion Candidates",
    description: "List healthy remote servers, newest first.",
    access: "read",
  },
  {
    name: "prepare_registry_promotion_curation",
    title: "Prepare Catalogue Candidate Review Status",
    description:
      "Reject, archive, or restore a candidate after browser approval.",
    access: "approval",
  },
  {
    name: "create_image_upload_url",
    title: "Create Image Upload URL",
    description: "Upload a public image for a later approved listing.",
    access: "write",
  },
];

beforeEach(() => fetchTools.mockResolvedValue({ tools }));
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

it("loads live tool descriptions and access labels without MCP installation", async () => {
  await renderWithApp(<AdminMcpTools />);
  const list = await screen.findByRole("list", {
    name: "Available Admin MCP tools",
  });
  expect(within(list).getAllByRole("listitem")).toHaveLength(3);
  expect(within(list).getByText("Read-only")).toBeTruthy();
  expect(within(list).getByText("Approval flow")).toBeTruthy();
  expect(within(list).getByText("Direct write")).toBeTruthy();
  expect(within(list).getByText(tools[0]!.description)).toBeTruthy();
  expect(fetchTools).toHaveBeenCalledWith("/admin-mcp/tools", {
    signal: expect.any(AbortSignal),
    cache: "no-store",
  });
});

it("combines search across names and descriptions with the access filter", async () => {
  await renderWithApp(<AdminMcpTools />);
  await screen.findByText("Showing 3 of 3 tools");
  fireEvent.change(screen.getByRole("searchbox", { name: "Search tools" }), {
    target: { value: "  ARCHIVE  " },
  });
  expect(screen.getByText("Showing 1 of 3 tools")).toBeTruthy();
  expect(screen.getByText("prepare_registry_promotion_curation")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Read-only" }));
  expect(
    screen.getByText("No tools match your search and filter."),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
  expect(screen.getByText("Showing 3 of 3 tools")).toBeTruthy();
  fireEvent.change(screen.getByRole("searchbox", { name: "Search tools" }), {
    target: { value: "list_registry" },
  });
  expect(screen.getByText("Showing 1 of 3 tools")).toBeTruthy();
  expect(screen.getByText("list_registry_promotion_candidates")).toBeTruthy();
});

it("shows loading until the server returns tool metadata", async () => {
  let resolve!: (value: { tools: AdminMcpTool[] }) => void;
  fetchTools.mockReturnValue(
    new Promise((done) => {
      resolve = done;
    }),
  );
  await renderWithApp(<AdminMcpTools />);
  expect(screen.getByRole("status").textContent).toContain(
    "Loading available tools",
  );
  resolve({ tools });
  expect(await screen.findByText("Showing 3 of 3 tools")).toBeTruthy();
});

it("keeps an unavailable directory distinct from an empty list and supports retry", async () => {
  fetchTools.mockRejectedValueOnce(new Error("unavailable"));
  await renderWithApp(<AdminMcpTools />);
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(screen.queryByText("No tools are enabled on this server.")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(await screen.findByText("Showing 3 of 3 tools")).toBeTruthy();
});

it("hides stale tool entries when a refresh fails", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await renderWithApp(<AdminMcpTools />, { queryClient });
  await screen.findByText("Showing 3 of 3 tools");
  fetchTools.mockRejectedValue(new Error("unavailable"));
  await queryClient.invalidateQueries({ queryKey: ["admin-mcp", "tools"] });
  await screen.findByRole("alert");
  expect(screen.queryByText("list_registry_promotion_candidates")).toBeNull();
});

it("shows an empty state when the server exposes no tools", async () => {
  fetchTools.mockResolvedValue({ tools: [] });
  await renderWithApp(<AdminMcpTools />);
  expect(
    await screen.findByText("No tools are enabled on this server."),
  ).toBeTruthy();
  await waitFor(() => expect(screen.queryByRole("searchbox")).toBeNull());
});
