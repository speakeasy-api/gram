import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminAIProduct } from "@gram/admin-client/models/components/adminaiproduct";
import { GramError } from "@gram/admin-client/models/errors/gramerror";
import { ServiceError } from "@gram/admin-client/models/errors/serviceerror";

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
        verificationError: "jwks_invalid",
        createdAt: new Date("2026-01-15T00:00:00Z"),
        updatedAt: new Date("2026-01-15T00:00:00Z"),
      },
    ],
    ...overrides,
  };
}

const CATALOG = [
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
];

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  // Answers the way the server does: deleted rows only when asked for, and
  // only rows whose name or id holds the search text.
  mocks.list.mockImplementation(
    async ({
      query,
      includeDeleted,
    }: {
      query?: string;
      includeDeleted: boolean;
    }) => ({
      products: CATALOG.filter(
        (product) =>
          (includeDeleted || !product.deleted) &&
          (!query ||
            [product.displayName, product.id].some((field) =>
              field.toLowerCase().includes(query.toLowerCase()),
            )),
      ),
      truncated: false,
    }),
  );
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
    expect(screen.queryByText("Old Tool")).toBeNull();
    expect(screen.queryByText("Deleted")).toBeNull();
    expect(
      screen.getByRole("link", { name: "Example CLI" }).getAttribute("href"),
    ).toBe("/ai-clients/products/example-cli");

    fireEvent.click(screen.getByRole("switch"));
    expect(await screen.findByText("Old Tool")).toBeTruthy();
    expect(screen.getByText("Deleted")).toBeTruthy();
    expect(screen.getByText("No category")).toBeTruthy();
    expect(mocks.list).toHaveBeenLastCalledWith({
      query: undefined,
      includeDeleted: true,
    });

    fireEvent.change(screen.getByLabelText("Search products"), {
      target: { value: "exa" },
    });
    await waitFor(() =>
      expect(mocks.list).toHaveBeenLastCalledWith({
        query: "exa",
        includeDeleted: true,
      }),
    );
    await waitFor(() => expect(screen.queryByText("Old Tool")).toBeNull());
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

  it("says when the database holds no products", async () => {
    mocks.list.mockResolvedValue({ products: [], truncated: false });
    await renderRouteTree(routeTree, {
      initialPath: "/ai-clients/products?deleted=true",
    });

    expect(
      await screen.findByText(
        "No products in the database. Servers use the built-in list until the catalog is seeded.",
      ),
    ).toBeTruthy();
  });

  it("points to deleted products when no active one exists", async () => {
    mocks.list.mockResolvedValue({ products: [], truncated: false });
    await renderRouteTree(routeTree, { initialPath: "/ai-clients/products" });

    expect(
      await screen.findByText(
        "No active products. Turn on Show deleted products to see removed ones.",
      ),
    ).toBeTruthy();
  });

  it("restores the search box when history steps back", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/ai-clients/products",
    });
    await screen.findByText("Example CLI");
    const box = () =>
      screen.getByLabelText("Search products") as HTMLInputElement;

    fireEvent.change(box(), { target: { value: "exa" } });
    await waitFor(() =>
      expect(router.state.location.search).toEqual({ q: "exa" }),
    );

    await act(() => router.navigate({ to: "/ai-clients/products" }));
    await waitFor(() => expect(box().value).toBe(""));

    await act(async () => {
      router.history.back();
    });
    await waitFor(() =>
      expect(router.state.location.search).toEqual({ q: "exa" }),
    );
    await waitFor(() => expect(box().value).toBe("exa"));
  });

  it("dims stale rows and makes them inert while new filters load", async () => {
    const { container } = await renderRouteTree(routeTree, {
      initialPath: "/ai-clients/products",
    });
    await screen.findByText("Example CLI");
    mocks.list.mockReturnValue(new Promise(() => {}));

    fireEvent.click(screen.getByRole("switch"));

    await waitFor(() =>
      expect(container.querySelector('[aria-busy="true"]')).not.toBeNull(),
    );
    const table = container.querySelector('[aria-busy="true"]');
    expect(table?.hasAttribute("inert")).toBe(true);
    expect(table?.textContent).toContain("Example CLI");
  });

  it("caps search text from the address at the API limit", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/ai-clients/products?q=${"a".repeat(300)}`,
    });

    await waitFor(() =>
      expect(mocks.list).toHaveBeenCalledWith({
        query: "a".repeat(256),
        includeDeleted: false,
      }),
    );
    expect(
      (screen.getByLabelText("Search products") as HTMLInputElement).maxLength,
    ).toBe(256);
  });

  it("says when the list is cut short", async () => {
    mocks.list.mockResolvedValue({
      products: [
        {
          id: "example-cli",
          displayName: "Example CLI",
          vendorKey: "example",
          documentCount: 0,
          deleted: false,
          updatedAt: new Date("2026-02-01T00:00:00Z"),
        },
      ],
      truncated: true,
    });
    await renderRouteTree(routeTree, { initialPath: "/ai-clients/products" });

    expect(
      await screen.findByText(
        "Only the first 1 products are shown. Search to find the rest.",
      ),
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
    expect(
      screen.getByText("Key set is not valid (jwks_invalid)"),
    ).toBeTruthy();
    expect(screen.getByText("Organizations blocking it")).toBeTruthy();
    expect(screen.getAllByText("Added by the built-in list")).toHaveLength(3);
    expect(screen.getByText("Not checked")).toBeTruthy();
  });

  it("names staff authors and explains blocks on a deleted product", async () => {
    mocks.get.mockResolvedValue(
      aProduct({
        deleted: true,
        createdBy: "user:staff-a",
        updatedBy: "user:staff-b",
      }),
    );
    await renderRouteTree(routeTree, {
      initialPath: "/ai-clients/products/example-cli",
    });

    expect(
      await screen.findByText(/blocks\s+on it stop applying\./),
    ).toBeTruthy();
    expect(screen.getByText("Organizations with a block")).toBeTruthy();
    expect(
      screen.getByText(
        "These blocks stop applying once servers load the change (about a minute).",
      ),
    ).toBeTruthy();
    expect(screen.queryByText("Organizations blocking it")).toBeNull();
    expect(screen.getByText("Added by Staff ID user:staff-a")).toBeTruthy();
    expect(screen.getByText("Changed by Staff ID user:staff-b")).toBeTruthy();
  });

  it("says when no product has the ID", async () => {
    mocks.get.mockRejectedValue(
      new ServiceError(
        {
          name: "not_found",
          id: "abc123",
          message: "product not found",
          fault: false,
          temporary: false,
          timeout: false,
        },
        {
          response: new Response("{}", {
            status: 404,
            headers: { "content-type": "application/json" },
          }),
          request: new Request("https://admin.example.test/"),
          body: "{}",
        },
      ),
    );
    await renderRouteTree(routeTree, {
      initialPath: "/ai-clients/products/missing",
    });

    expect(await screen.findByText("No product has this ID.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Back to products" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  });

  it("treats a 404 that is not the server's answer as a failure", async () => {
    mocks.get.mockRejectedValue(
      new GramError("Not Found", {
        response: new Response("<html>Not Found</html>", {
          status: 404,
          headers: { "content-type": "text/html" },
        }),
        request: new Request("https://admin.example.test/"),
        body: "<html>Not Found</html>",
      }),
    );
    await renderRouteTree(routeTree, {
      initialPath: "/ai-clients/products/missing",
    });

    expect(await screen.findByText(/Could not load this product/)).toBeTruthy();
    expect(screen.queryByText("No product has this ID.")).toBeNull();
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
  });
});
