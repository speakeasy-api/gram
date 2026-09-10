import {
  cleanup,
  render,
  screen,
  fireEvent,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
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
beforeEach(() => {
  list.mockReset().mockResolvedValue({ result: { items: [] } });
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
it("renders each issuer's own View link and paginates the catalog", async () => {
  let finishPage = () => {};
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
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishPage = () =>
            resolve({
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
        }),
    );
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
  expect(screen.getByText("First")).toBeTruthy();
  expect(
    (screen.getByRole("button", { name: "Next" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  expect(
    (screen.getByRole("button", { name: "Previous" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  finishPage();
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

it("shows a refetch error instead of the stale empty state", async () => {
  list.mockResolvedValueOnce({ result: { items: [] } });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <IssuerList />
    </QueryClientProvider>,
  );
  expect(await screen.findByText("No issuers found")).toBeTruthy();
  list.mockRejectedValue(new Error("Refetch refused"));
  await client.invalidateQueries();
  expect(await screen.findByText("Refetch refused")).toBeTruthy();
  expect(screen.queryByText("No issuers found")).toBeNull();
});
