import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminAIProduct } from "@gram/admin-client/models/components/adminaiproduct";
import { GramError } from "@gram/admin-client/models/errors/gramerror";

import { routeTree } from "@/routeTree.gen";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  list: vi.fn(),
  get: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return { ...actual, getSession: mocks.getSession };
});
vi.mock("@gram/admin-client/react-query/adminListAIProducts.core", () => ({
  buildAdminListAIProductsQuery: (_client: unknown, request: unknown) => ({
    queryKey: ["@gram/admin-client", "admin", "listAIProducts", request],
    queryFn: () => mocks.list(request),
  }),
}));
vi.mock("@gram/admin-client/react-query/adminGetAIProduct.core", () => ({
  buildAdminGetAIProductQuery: (_client: unknown, request: { id: string }) => ({
    queryKey: ["@gram/admin-client", "admin", "getAIProduct", request],
    queryFn: () => mocks.get(request.id),
  }),
}));

// Invented values only: this repository is public.
function aProduct(overrides: Partial<AdminAIProduct> = {}): AdminAIProduct {
  return {
    id: "example-cli",
    displayName: "Example CLI",
    category: "harness",
    vendorKey: "example",
    bundleIds: ["com.example.cli"],
    binaries: ["example"],
    configDirs: ["~/.example"],
    processNames: [],
    clientInfoNames: ["example-cli"],
    deleted: false,
    createdAt: new Date("2026-01-15T00:00:00Z"),
    updatedAt: new Date("2026-02-01T00:00:00Z"),
    blockedOrganizationCount: 3,
    documents: [
      {
        id: "11111111-1111-4111-8111-111111111111",
        url: "https://example.test/*/client.json",
        displayName: "Example connectors",
        enabled: false,
        isPattern: true,
        deleted: false,
        createdAt: new Date("2026-01-15T00:00:00Z"),
        updatedAt: new Date("2026-01-15T00:00:00Z"),
      },
      {
        id: "22222222-2222-4222-8222-222222222222",
        url: "https://example.test/client.json",
        displayName: "Example CLI",
        enabled: true,
        isPattern: false,
        deleted: false,
        verificationError: "fetch_failed",
        createdAt: new Date("2026-01-15T00:00:00Z"),
        updatedAt: new Date("2026-01-15T00:00:00Z"),
      },
    ],
    ...overrides,
  };
}

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.list.mockResolvedValue({
    products: [
      {
        id: "example-cli",
        displayName: "Example CLI",
        category: "harness",
        vendorKey: "example",
        documentCount: 2,
        deleted: false,
        updatedAt: new Date("2026-02-01T00:00:00Z"),
      },
      {
        id: "old-tool",
        displayName: "Old Tool",
        vendorKey: "old",
        documentCount: 0,
        deleted: true,
        updatedAt: new Date("2026-02-01T00:00:00Z"),
      },
    ],
    truncated: false,
  });
  mocks.get.mockResolvedValue(aProduct());
});

afterEach(() => {
  cleanup();
});

describe("AIProductList", () => {
  it("lists products and keeps default filters out of the address", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/ai-clients",
    });

    expect(await screen.findByText("Example CLI")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/ai-clients/products");
    expect(router.state.location.searchStr).toBe("");
    expect(mocks.list).toHaveBeenCalledWith({
      query: undefined,
      includeDeleted: false,
    });
    expect(screen.getByText("Coding agent")).toBeTruthy();
    expect(screen.getByText("Sign-in only")).toBeTruthy();
    expect(screen.getByText("Deleted")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Example CLI" }).getAttribute("href"),
    ).toBe("/ai-clients/products/example-cli");

    fireEvent.change(screen.getByLabelText("Search products"), {
      target: { value: "exa" },
    });
    fireEvent.click(screen.getByRole("switch"));
    await waitFor(() =>
      expect(mocks.list).toHaveBeenLastCalledWith({
        query: "exa",
        includeDeleted: true,
      }),
    );
    expect(router.state.location.search).toEqual({ q: "exa", deleted: true });
  });

  it("says so when nothing matches", async () => {
    mocks.list.mockResolvedValue({ products: [], truncated: false });
    await renderRouteTree(routeTree, {
      initialPath: "/ai-clients/products?q=zzz",
    });

    expect(
      await screen.findByText("No products match your search."),
    ).toBeTruthy();
  });

  it("offers a retry when the list fails", async () => {
    mocks.list.mockRejectedValue(new Error("boom"));
    await renderRouteTree(routeTree, { initialPath: "/ai-clients/products" });

    expect(await screen.findByText(/Could not load products/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
  });
});

describe("AIProductDetail", () => {
  it("shows signatures, documents and the block count", async () => {
    await renderRouteTree(routeTree, {
      initialPath: "/ai-clients/products/example-cli",
    });

    expect(
      await screen.findByRole("heading", { name: "Example CLI", level: 1 }),
    ).toBeTruthy();
    expect(mocks.get).toHaveBeenCalledWith("example-cli");
    expect(screen.getByText("com.example.cli")).toBeTruthy();
    expect(screen.getByText("~/.example")).toBeTruthy();
    expect(screen.getByText("3")).toBeTruthy();
    expect(screen.getByText("Pattern")).toBeTruthy();
    expect(screen.getByText("Off")).toBeTruthy();
    expect(screen.getByText("On")).toBeTruthy();
    expect(screen.getByText("Check failed")).toBeTruthy();
    expect(screen.getByText("Reason: fetch_failed")).toBeTruthy();
    expect(screen.getByText("Not checked")).toBeTruthy();
  });

  it("says when no product has the ID", async () => {
    mocks.get.mockRejectedValue(
      new GramError("not found", {
        response: new Response(null, { status: 404 }),
        request: new Request("https://admin.example.test/"),
        body: "",
      }),
    );
    await renderRouteTree(routeTree, {
      initialPath: "/ai-clients/products/missing",
    });

    expect(await screen.findByText("No product has this ID.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Back to products" })).toBeTruthy();
  });
});
