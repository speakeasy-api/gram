import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Explore from "./Explore";

const testState = vi.hoisted(() => ({
  isPending: false,
  /** How PostHog answers for the Explore rollout flag. */
  flagStatus: "enabled" as "enabled" | "disabled" | "loading",
  isError: false,
  datasets: [] as unknown[],
  refetch: vi.fn().mockResolvedValue(undefined),
  /** How many times the page asked for the catalog. */
  describeCalls: 0,
  /** Every body handed to the query hook, in call order; null is "nothing to run". */
  bodies: [] as (AnalyticsQueryPayload | null)[],
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: testState.flagStatus }),
}));
vi.mock("@gram/client/react-query/analyticsDescribe.js", () => ({
  useAnalyticsDescribe: () => {
    testState.describeCalls += 1;
    return {
      isPending: testState.isPending,
      isError: testState.isError,
      data:
        testState.isPending || testState.isError
          ? undefined
          : { datasets: testState.datasets },
      refetch: testState.refetch,
    };
  },
}));
vi.mock("./useRunQuery", () => ({
  useRunQuery: (body: AnalyticsQueryPayload | null) => {
    testState.bodies.push(body);
    return {
      data: undefined,
      error: null,
      isError: false,
      isPending: true,
      isFetching: body !== null,
      isPlaceholderData: false,
    };
  },
}));
// The debounce is timing, tested on its own; here the settled spec is the spec.
vi.mock("@/hooks/useDebouncedValue", () => ({
  useDebouncedValue: <T,>(value: T) => value,
}));
vi.mock("@/components/page-templates", () => ({
  WorkbenchPage: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/page-layout", () => ({
  Page: { Eyebrow: () => null },
}));
vi.mock("@/components/release-stage-badge", () => ({
  ReleaseStageBadge: ({ stage }: { stage: string }) => <span>{stage}</span>,
}));

const sessions: AnalyticsDataset = {
  name: "sessions",
  kind: "event",
  grain: "session",
  description: "One row per agent session.",
  summaryField: "user",
  fields: [
    {
      name: "user",
      type: "string",
      role: "dimension",
      operators: ["equals", "in"],
    },
    { name: "surface", type: "string", role: "dimension", operators: ["in"] },
    {
      name: "turn_count",
      type: "int64",
      role: "measure",
      aggregations: ["sum", "avg"],
    },
  ],
};

const toolCalls: AnalyticsDataset = {
  name: "tool_calls",
  kind: "event",
  grain: "tool call",
  description: "One row per tool call.",
  summaryField: "tool_name",
  fields: [
    { name: "tool_name", type: "string", role: "dimension", operators: ["in"] },
    {
      name: "duration_ms",
      type: "float64",
      role: "measure",
      unit: "ms",
      aggregations: ["p95"],
    },
  ],
};

describe("Explore", () => {
  beforeEach(() => {
    testState.isPending = false;
    testState.flagStatus = "enabled";
    testState.isError = false;
    testState.datasets = [sessions, toolCalls];
    testState.refetch.mockClear();
    testState.bodies = [];
  });

  afterEach(() => {
    cleanup();
  });

  it("opens on the catalog's first dataset with its defaults", () => {
    render(<Explore />);

    expect(screen.getByRole("heading", { name: "Explore" })).toBeTruthy();
    expect(screen.getByText("preview")).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "Dataset" }).textContent).toBe(
      "sessions",
    );
    expect(screen.getByText("One row per agent session.")).toBeTruthy();
    expect(screen.getByText("· session grain")).toBeTruthy();
    expect(
      screen.getByRole("combobox", { name: "Aggregation" }).textContent,
    ).toBe("count");
    expect(screen.getByText("of all rows")).toBeTruthy();
    // The summary field is the opening breakdown.
    expect(screen.getByText("user")).toBeTruthy();
  });

  it("runs both shapes of the opening query without a run button", () => {
    render(<Explore />);

    // The opening spec is a line chart, so a bucketed chart query and a
    // whole-window summary query both run, over the short default window.
    const [chart, summary] = testState.bodies;
    expect(chart?.dataset).toBe("sessions");
    expect(chart?.grain).toBe("hour");
    expect(chart?.dimensions).toEqual(["user"]);
    expect(chart?.measures).toEqual([
      { op: "count", field: undefined, alias: "count" },
    ]);
    expect(summary?.grain).toBe("none");
    expect(chart!.to.getTime() - chart!.from.getTime()).toBe(24 * 3_600_000);
  });

  it("stops asking for a chart once the chart type is a table", () => {
    render(<Explore />);
    testState.bodies = [];

    fireEvent.click(screen.getByRole("button", { name: "Table" }));
    const [chart, summary] = testState.bodies;
    expect(chart).toBeNull();
    expect(summary?.grain).toBe("none");
  });

  it("adds and removes filter rows", () => {
    render(<Explore />);

    expect(screen.queryByRole("combobox", { name: "Filter field" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    expect(screen.getByRole("combobox", { name: "Filter field" })).toBeTruthy();
    // No field picked yet, so no operator or value control.
    expect(
      screen.queryByRole("combobox", { name: "Filter operator" }),
    ).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Remove filter" }));
    expect(screen.queryByRole("combobox", { name: "Filter field" })).toBeNull();
    expect(screen.getByRole("button", { name: "Add filter" })).toBeTruthy();
  });

  it("adds and removes measure rows, and no measure means rows", () => {
    render(<Explore />);

    fireEvent.click(
      screen.getByRole("button", { name: "Add another measure" }),
    );
    expect(
      screen.getAllByRole("combobox", { name: "Aggregation" }),
    ).toHaveLength(2);

    fireEvent.click(
      screen.getAllByRole("button", { name: "Remove measure" })[0]!,
    );
    fireEvent.click(
      screen.getAllByRole("button", { name: "Remove measure" })[0]!,
    );
    expect(screen.queryByRole("combobox", { name: "Aggregation" })).toBeNull();
    expect(screen.getByText(/Nothing measured/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add measure" })).toBeTruthy();

    // With nothing measured the query asks for rows at the dataset's grain.
    const last = testState.bodies.at(-1);
    expect(last?.ungrouped).toBe(true);
    expect(last?.measures).toBeUndefined();
  });

  it("shows the catalog loading", () => {
    testState.isPending = true;
    render(<Explore />);

    expect(screen.getByLabelText("Loading the catalog")).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "Dataset" })).toBeNull();
  });

  it("offers a retry when the catalog fails to load", () => {
    testState.isError = true;
    render(<Explore />);

    expect(screen.getByText("The catalog did not load")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(testState.refetch).toHaveBeenCalledTimes(1);
  });

  it("says so when the catalog has no datasets, and runs nothing", () => {
    testState.datasets = [];
    render(<Explore />);

    expect(screen.getByText("No datasets yet")).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "Dataset" })).toBeNull();
    expect(testState.bodies.every((body) => body === null)).toBe(true);
  });

  it("stays closed to an organization the rollout has not reached", () => {
    testState.flagStatus = "disabled";
    testState.describeCalls = 0;
    render(<Explore />);

    expect(screen.getByText("Explore is not available yet")).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "Dataset" })).toBeNull();
    expect(testState.describeCalls, "it never asks for the catalog").toBe(0);
  });

  it("waits rather than saying no while the flag is still loading", () => {
    testState.flagStatus = "loading";
    render(<Explore />);

    expect(screen.getByLabelText("Loading the catalog")).toBeTruthy();
    expect(screen.queryByText("Explore is not available yet")).toBeNull();
  });
});
