import { unrecognized } from "@gram/client/types/unrecognized.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SpendBreakdownData } from "./spend-breakdown-adapter";
import { SpendBreakdownSection } from "./spend-breakdown-section";

const mocks = vi.hoisted(() => ({
  fetchSpend: vi.fn<() => Promise<SpendBreakdownData>>(),
}));

vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/getSpendBreakdown.js", () => ({
  buildGetSpendBreakdownQuery: () => ({
    queryKey: ["spend"],
    queryFn: mocks.fetchSpend,
  }),
}));
vi.mock("@/components/page-layout", () => {
  const Box = ({ children }: { children: ReactNode }) => <>{children}</>;
  const Section = Object.assign(
    ({ children }: { children: ReactNode }) => <section>{children}</section>,
    { Title: Box, Description: Box, Body: Box },
  );
  const Toolbar = Object.assign(
    ({ children }: { children: ReactNode }) => <div>{children}</div>,
    {
      Row: Box,
      Leading: Box,
      Actions: Box,
      Refresh: ({ onRefresh }: { onRefresh: () => void }) => (
        <button onClick={onRefresh}>Refresh estimates</button>
      ),
    },
  );
  return { Page: { Section, Toolbar } };
});
vi.mock("@/components/ui/MultiSelect", () => ({
  MultiSelect: () => null,
}));
vi.mock("@/components/stacked-time-series-panel", () => ({
  StackedTimeSeriesPanel: () => <div>Spend chart</div>,
}));

function response(): SpendBreakdownData {
  const day = new Date("2026-09-19T00:00:00Z");
  return {
    availability: "available",
    currency: "USD",
    pricingBasis: "current_payg_list_price",
    queriedAt: day,
    window: { from: day, to: day },
    billingCycles: [],
    totalCostUsd: "1",
    products: [
      {
        id: "agent_session_storage",
        label: "Agent session storage",
        unit: "stokens",
        quantity: "1000000",
        rateQuantity: "1000000",
        rateUsd: "0.35",
        costUsd: "1",
        buckets: [],
      },
    ],
  };
}

function renderSection() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <SpendBreakdownSection />
    </QueryClientProvider>,
  );
  return client;
}

describe("SpendBreakdownSection unsupported responses", () => {
  beforeEach(() => {
    mocks.fetchSpend.mockReset();
  });
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it.each(["availability", "currency", "pricingBasis", "id", "unit"] as const)(
    "offers a page reload instead of a partial estimate for unknown %s",
    async (field) => {
      const data = response();
      if (field === "id" || field === "unit") {
        data.products.push({
          ...data.products[0]!,
          label: "New product",
          [field]: unrecognized("future_value"),
        });
      } else {
        data[field] = unrecognized("future_value");
      }
      mocks.fetchSpend.mockResolvedValue(data);
      const reload = vi
        .spyOn(window.location, "reload")
        .mockImplementation(() => {});
      renderSection();

      expect((await screen.findByRole("alert")).textContent).toContain(
        "Refresh the page to display these newer spend estimates.",
      );
      expect(screen.queryByText("Selected estimated cost")).toBeNull();
      expect(screen.queryByText("Spend chart")).toBeNull();
      expect(screen.queryByText("Agent session storage")).toBeNull();
      expect(screen.queryByRole("button", { name: /^retry$/i })).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Refresh page" }));
      expect(reload).toHaveBeenCalledOnce();
      expect(mocks.fetchSpend).toHaveBeenCalledOnce();
    },
  );

  it("retains valid estimates and offers reload when a newer response is unsupported", async () => {
    mocks.fetchSpend.mockResolvedValueOnce(response());
    const client = renderSection();
    expect(await screen.findByText("Selected estimated cost")).toBeTruthy();

    const newer = response();
    newer.products.push({
      ...newer.products[0]!,
      id: unrecognized("future_product"),
      label: "New product",
      costUsd: "9",
    });
    newer.totalCostUsd = "10";
    mocks.fetchSpend.mockResolvedValue(newer);
    await act(async () => {
      await client.refetchQueries({ queryKey: ["spend"] });
    });

    expect((await screen.findByRole("alert")).textContent).toContain(
      "Refresh the page to display these newer spend estimates. Showing the last loaded data.",
    );
    expect(screen.getByRole("button", { name: "Refresh page" })).toBeTruthy();
    expect(screen.getByText("Selected estimated cost")).toBeTruthy();
    expect(screen.getAllByText("$1.00")).toHaveLength(2);
    expect(screen.queryByText("New product")).toBeNull();
    expect(screen.queryByText("$10.00")).toBeNull();
  });

  it.each([false, true])(
    "keeps Retry for ordinary failures with retained data: %s",
    async (retained) => {
      if (retained) mocks.fetchSpend.mockResolvedValueOnce(response());
      mocks.fetchSpend.mockRejectedValue(new Error("Network unavailable"));
      const client = renderSection();
      if (retained) {
        await screen.findByText("Selected estimated cost");
        await act(async () => {
          await client.refetchQueries({ queryKey: ["spend"] });
        });
      }
      expect((await screen.findByRole("alert")).textContent).toContain(
        retained
          ? "Couldn't refresh estimated spend."
          : "Couldn't load estimated spend.",
      );
      expect(screen.queryByRole("button", { name: "Refresh page" })).toBeNull();
      mocks.fetchSpend.mockResolvedValue(response());
      fireEvent.click(screen.getByRole("button", { name: /^retry$/i }));
      await screen.findByText("Selected estimated cost");
      await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    },
  );
});
