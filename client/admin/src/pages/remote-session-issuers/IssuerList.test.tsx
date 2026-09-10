import {
  cleanup,
  render,
  screen,
  fireEvent,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { IssuerList } from "./IssuerList";
const list = vi.hoisted(() => vi.fn());
vi.mock("@/lib/gramAdminClient", () => ({
  adminListGlobalIssuersQuery: (request: unknown) => ({
    queryKey: ["issuers", request],
    queryFn: () => list(request),
  }),
}));
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({
    params,
    children,
  }: {
    params: { issuerId: string };
    children: React.ReactNode;
  }) => <a href={`/remote-session-issuers/${params.issuerId}`}>{children}</a>,
}));
vi.mock("./IssuerEditor", () => ({
  IssuerEditor: () => <div>Create form</div>,
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
it("renders each issuer's own View link and paginates the catalog", async () => {
  list
    .mockResolvedValueOnce({
      result: {
        items: [
          {
            issuer: {
              id: "one",
              name: "First",
              issuer: "https://first.example",
              slug: "first",
            },
            globalClientCount: 2,
            tenantClientCount: 3,
          },
        ],
        nextCursor: "page2",
      },
    })
    .mockResolvedValueOnce({
      result: {
        items: [
          {
            issuer: {
              id: "two",
              name: "Second",
              issuer: "https://second.example",
              slug: "second",
            },
            globalClientCount: 0,
            tenantClientCount: 1,
          },
        ],
      },
    });
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <IssuerList />
    </QueryClientProvider>,
  );
  expect(
    (await screen.findByRole("link", { name: "View" })).getAttribute("href"),
  ).toBe("/remote-session-issuers/one");
  fireEvent.click(screen.getByRole("button", { name: "Next" }));
  await waitFor(() =>
    expect(list).toHaveBeenLastCalledWith({ cursor: "page2", limit: 50 }),
  );
  expect(await screen.findByText("Second")).toBeTruthy();
  expect(screen.getByRole("link", { name: "View" }).getAttribute("href")).toBe(
    "/remote-session-issuers/two",
  );
});
it("shows query errors without claiming the catalog is empty", async () => {
  list.mockRejectedValue(new Error("Catalog unavailable"));
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <IssuerList />
    </QueryClientProvider>,
  );
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Catalog unavailable",
  );
  expect(screen.queryByText("No issuers found")).toBeNull();
});
