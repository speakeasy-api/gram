import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { PaygPriceList } from "./payg-price-list";

vi.mock("@gram/client/react-query/getUsageTiers.js", () => ({
  useGetUsageTiers: () => ({
    data: {
      payg: {
        tumPricePerMillionUsd: "0.35",
        riskScanPricePerMillionUsd: "0.000123",
        mcpEgressPricePerGibUsd: "0.05",
      },
    },
    isLoading: false,
    isError: false,
  }),
}));

afterEach(cleanup);

it("nests metered rate headings beneath Pricing and preserves fractional rates", () => {
  render(<PaygPriceList />);
  expect(
    screen.getByRole("heading", { name: "Pricing", level: 3 }),
  ).toBeTruthy();
  for (const name of [
    "Agent session storage",
    "Risk scanning",
    "MCP gateway",
  ]) {
    expect(screen.getByRole("heading", { name, level: 4 })).toBeTruthy();
  }
  expect(screen.getByText("$0.000123")).toBeTruthy();
});
