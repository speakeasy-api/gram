import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  type QueryFunctionContext,
} from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { SpendBreakdown } from "./SpendBreakdown";
import type { AdminSpendBreakdown } from "./spendBreakdownUtils";

const mocks = vi.hoisted(() => ({ fetch: vi.fn(), search: {} }));
vi.mock("@/lib/gramAdminClient", () => ({
  organizationSpendBreakdownQuery: () => ({
    queryKey: ["spend"],
    queryFn: mocks.fetch,
  }),
}));
vi.mock("@tanstack/react-router", () => ({
  getRouteApi: () => ({
    useSearch: () => mocks.search,
    useNavigate: () => vi.fn(),
  }),
}));
vi.mock("./SpendBreakdownChart", () => ({ SpendBreakdownChart: () => null }));
vi.mock("./SpendProductsTable", () => ({ SpendProductsTable: () => null }));
vi.mock("./MeterUsagePeriod", () => ({ MeterUsagePeriod: () => null }));

const valid: AdminSpendBreakdown = {
  billingCycles: [],
  currency: "USD",
  pricingBasis: "current_payg_list_price",
  queriedAt: new Date("2026-09-02T12:00:00Z"),
  totalCostUsd: "12",
  window: { from: new Date("2026-09-01"), to: new Date("2026-09-03") },
  products: [],
};
const clients: QueryClient[] = [];
function mount(
  retry: boolean | number | ((count: number, error: Error) => boolean) = false,
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry, retryDelay: 0, gcTime: Infinity } },
  });
  clients.push(client);
  render(
    <QueryClientProvider client={client}>
      <SpendBreakdown organizationID="fixture-org" />
    </QueryClientProvider>,
  );
  return client;
}
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  vi.restoreAllMocks();
  mocks.fetch.mockReset();
});

it("retains the last estimate after malformed data with an advancing clock", async () => {
  mocks.fetch.mockResolvedValue(valid);
  const client = mount();
  await waitFor(() => expect(client.getQueryData(["spend"])).toEqual(valid));
  let now = Date.now();
  vi.spyOn(Date, "now").mockImplementation(() => ++now);
  mocks.fetch.mockResolvedValue({ ...valid, totalCostUsd: "invalid" });
  await act(async () => {
    await client.refetchQueries({ queryKey: ["spend"] });
  });
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Showing the last successful estimate.",
  );
});

it("does not retry malformed data and allows a manual retry", async () => {
  mocks.fetch.mockResolvedValue({ ...valid, totalCostUsd: "invalid" });
  mount(3);
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Invalid exact decimal: invalid",
  );
  expect(mocks.fetch).toHaveBeenCalledTimes(1);
  mocks.fetch.mockResolvedValue(valid);
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  expect(mocks.fetch).toHaveBeenCalledTimes(2);
});

it.each([false, 1, true, "callback"] as const)(
  "preserves ordinary retry policy %s and query context",
  async (policy) => {
    const error = new Error("network unavailable");
    const retry = vi.fn((count: number) => count < 1);
    mocks.fetch.mockRejectedValueOnce(error).mockResolvedValue(valid);
    const client = mount(policy === "callback" ? retry : policy);
    await waitFor(() =>
      expect(client.getQueryState(["spend"])?.fetchStatus).toBe("idle"),
    );
    expect(mocks.fetch).toHaveBeenCalledTimes(policy === false ? 1 : 2);
    if (policy === "callback") expect(retry).toHaveBeenCalledWith(0, error);
    const context: QueryFunctionContext = mocks.fetch.mock.calls[0]![0];
    expect(context.queryKey).toEqual(["spend"]);
    expect(context.client).toBe(client);
    expect(context.signal).toBeInstanceOf(AbortSignal);
  },
);
