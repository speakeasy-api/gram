import type { InferenceSpendHistory } from "@gram/client/models/components/inferencespendhistory.js";
import type { InferenceSpendMonth } from "@gram/client/models/components/inferencespendmonth.js";
import { RFCDate } from "@gram/client/types/rfcdate.js";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  query: vi.fn(),
}));

vi.mock("@gram/client/react-query/getInferenceSpendHistory.js", () => ({
  useGetInferenceSpendHistory: (...args: unknown[]) => mocks.query(...args),
}));

vi.mock("@/components/page-layout", () => {
  const Section = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.Title = ({ children }: { children: ReactNode }) => (
    <h2>{children}</h2>
  );
  Section.Description = ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  );
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  return { Page: { Section } };
});

import { InferenceSpendHistorySection } from "./inference-spend-history-section";

function month(
  overrides: Partial<InferenceSpendMonth> = {},
): InferenceSpendMonth {
  return {
    current: false,
    keySpend: [],
    monthStart: new Date("2026-07-01T00:00:00.000Z"),
    monthEnd: new Date("2026-08-01T00:00:00.000Z"),
    spendUsd: "0.000000",
    ...overrides,
  };
}

function history(months: InferenceSpendMonth[]): InferenceSpendHistory {
  return { months };
}

describe("InferenceSpendHistorySection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(cleanup);

  it("opts out of the shared throwOnError so a failed read stays on the page", () => {
    mocks.query.mockReturnValue({
      data: undefined,
      isError: false,
      isFetching: false,
      refetch: vi.fn(),
    });

    render(<InferenceSpendHistorySection />);

    expect(mocks.query).toHaveBeenCalledWith(undefined, undefined, {
      throwOnError: false,
    });
  });

  it("lists recorded months with key totals and marks the current month", () => {
    mocks.query.mockReturnValue({
      data: history([
        month({
          current: true,
          monthStart: new Date("2026-08-01T00:00:00.000Z"),
          monthEnd: new Date("2026-09-01T00:00:00.000Z"),
          spendUsd: "1.500000",
          recordedThrough: new RFCDate("2026-08-20"),
          keySpend: [
            { keyType: "chat", spendUsd: "1.200000" },
            { keyType: "internal", spendUsd: "0.300000" },
          ],
        }),
        month({
          spendUsd: "4.500000",
          recordedThrough: new RFCDate("2026-07-31"),
          keySpend: [
            { keyType: "chat", spendUsd: "4.000000" },
            { keyType: "internal", spendUsd: "0.500000" },
          ],
        }),
      ]),
      isError: false,
      isFetching: false,
      refetch: vi.fn(),
    });

    render(<InferenceSpendHistorySection />);

    expect(
      screen.getByRole("heading", { name: /inference spend/i }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        /Customer-facing inference is assistants and the other AI-powered dashboard experiences/,
      ),
    ).toBeTruthy();
    expect(screen.getByText("Customer-facing inference")).toBeTruthy();
    expect(screen.getByText("Security inference")).toBeTruthy();
    expect(screen.getByText("August 2026 (current)")).toBeTruthy();
    expect(screen.getByText("July 2026")).toBeTruthy();
    expect(screen.getByText("$1.50")).toBeTruthy();
    expect(screen.getByText("$4.50")).toBeTruthy();
    expect(
      screen.getByText(
        /Completed days through August 20, 2026; today isn't counted yet./,
      ),
    ).toBeTruthy();
  });

  it("keeps a missing key as an em dash rather than a fabricated zero", () => {
    mocks.query.mockReturnValue({
      data: history([
        month({
          current: true,
          monthStart: new Date("2026-08-01T00:00:00.000Z"),
          monthEnd: new Date("2026-09-01T00:00:00.000Z"),
          spendUsd: "2.000000",
          keySpend: [{ keyType: "chat", spendUsd: "2.000000" }],
        }),
      ]),
      isError: false,
      isFetching: false,
      refetch: vi.fn(),
    });

    render(<InferenceSpendHistorySection />);

    expect(screen.getAllByText("$2.00").length).toBe(2);
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });

  it("offers a retry when the history has never loaded", () => {
    const refetch = vi.fn();
    mocks.query.mockReturnValue({
      data: undefined,
      isError: true,
      isFetching: false,
      refetch,
    });

    render(<InferenceSpendHistorySection />);

    expect(
      screen.getByText(/Couldn't load inference spend history./),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "RETRY" })).toBeTruthy();
  });
});
