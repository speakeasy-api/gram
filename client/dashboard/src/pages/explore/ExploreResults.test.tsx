import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { specForDataset, type ExploreSpec } from "./exploreModel";
import { ExploreResults, type RunQuery } from "./ExploreResults";

vi.mock("react-chartjs-2", () => ({
  Chart: ({ data }: { data: { datasets: { label: string }[] } }) => (
    <div data-testid="chart">
      {data.datasets.map((dataset) => dataset.label).join(",")}
    </div>
  ),
}));
vi.mock("@/lib/theme", () => ({ useIsDarkTheme: () => false }));
vi.mock("@/components/chart/useSeriesColors", () => ({
  useSeriesColors: () => ["#000", "#111"],
}));

const dataset: AnalyticsDataset = {
  name: "sessions",
  kind: "event",
  grain: "session",
  description: "One row per agent session.",
  fields: [
    {
      name: "user",
      type: "string",
      role: "dimension",
      default: true,
      operators: ["in"],
    },
    {
      name: "duration_seconds",
      type: "float64",
      role: "measure",
      default: false,
      unit: "s",
      aggregations: ["sum"],
    },
  ],
};

function query(overrides: Record<string, unknown> = {}): RunQuery {
  return {
    data: undefined,
    error: null,
    isError: false,
    isPending: true,
    isFetching: false,
    ...overrides,
  } as unknown as RunQuery;
}

function loaded(
  rows: Record<string, unknown>[],
  extra: Record<string, unknown> = {},
): RunQuery {
  return query({
    data: { dataset: "sessions", plan: "p", rows },
    isPending: false,
    ...extra,
  });
}

function spec(overrides: Partial<ExploreSpec> = {}): ExploreSpec {
  return { ...specForDataset(dataset), ...overrides };
}

describe("ExploreResults", () => {
  afterEach(() => {
    cleanup();
  });

  it("says it is busy and blanks the panel while a run loads", () => {
    const { container } = render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "table" })}
        chart={query()}
        summary={query({ isFetching: true })}
      />,
    );
    expect(container.querySelector('section[aria-busy="true"]')).toBeTruthy();
    expect(screen.queryByText("No rows to show")).toBeNull();
  });

  it("tables a loaded run without naming a plan or dataset in the header", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "table" })}
        chart={query()}
        summary={loaded([{ user: "ann", count: 1250 }])}
      />,
    );
    expect(screen.queryByText("sessions")).toBeNull();
    expect(screen.getByText("ann")).toBeTruthy();
    expect(screen.getByText("1.3K")).toBeTruthy();
  });

  it("shows the server's reason when the query did not run", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "table" })}
        chart={query()}
        summary={query({
          isPending: false,
          isError: true,
          error: new Error(
            'unknown_field: no such field (dimensions[0] = "dept")',
          ),
        })}
      />,
    );
    expect(screen.getByText("The query did not run")).toBeTruthy();
    expect(screen.getByText(/unknown_field/)).toBeTruthy();
  });

  it("uses one empty state for no rows", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "table" })}
        chart={query()}
        summary={loaded([])}
      />,
    );
    expect(screen.getByText("No rows to show")).toBeTruthy();
  });

  it("tables the summary with a column per dimension and measure, values formatted by unit", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({
          chartType: "table",
          measures: [
            { op: "count", field: "" },
            { op: "sum", field: "duration_seconds" },
          ],
        })}
        chart={query()}
        summary={loaded([
          { user: "ann", count: 3, sum_duration_seconds: 90 },
          { user: "", count: 1, sum_duration_seconds: 4.25 },
        ])}
      />,
    );
    expect(screen.getByText("user")).toBeTruthy();
    expect(screen.getByText("COUNT")).toBeTruthy();
    expect(screen.getByText("SUM(duration_seconds)")).toBeTruthy();
    expect(screen.getByText("ann")).toBeTruthy();
    expect(screen.getByText("90 s")).toBeTruthy();
    expect(screen.getByText("4.3 s")).toBeTruthy();
    expect(screen.getByText("—")).toBeTruthy();
  });

  it("tiles a number chart with one figure per measure", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "number" })}
        chart={query()}
        summary={loaded([{ count: 12500 }])}
      />,
    );
    expect(screen.getByText("COUNT")).toBeTruthy();
    expect(screen.getByText("12.5K")).toBeTruthy();
  });

  it("draws a timeseries from the chart query with the summary tabled beneath", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "line" })}
        chart={loaded([
          { time_bucket: "2026-09-14T10:00:00Z", user: "ann", count: 2 },
          { time_bucket: "2026-09-14T10:00:00Z", user: "bob", count: 1 },
        ])}
        summary={loaded([
          { user: "ann", count: 2 },
          { user: "bob", count: 1 },
        ])}
      />,
    );
    expect(screen.getByTestId("chart").textContent).toBe("ann,bob");
    expect(screen.getByText("Summary")).toBeTruthy();
    expect(screen.getByText("ann")).toBeTruthy();
  });

  it("refuses to chart measures with different units", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({
          chartType: "line",
          measures: [
            { op: "count", field: "" },
            { op: "sum", field: "duration_seconds" },
          ],
        })}
        chart={loaded([
          { time_bucket: "t", count: 1, sum_duration_seconds: 2 },
        ])}
        summary={loaded([{ count: 1, sum_duration_seconds: 2 }])}
      />,
    );
    expect(screen.getByText("These measures do not share a unit")).toBeTruthy();
    expect(screen.queryByTestId("chart")).toBeNull();
  });

  it("tables projected rows, time first, when nothing is measured", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "line", measures: [] })}
        chart={query()}
        summary={loaded([{ user: "ann", time: "2026-09-14T10:15:30Z" }])}
      />,
    );
    const headers = screen
      .getAllByRole("columnheader")
      .map((h) => h.textContent);
    expect(headers[0]).toBe("time");
    expect(headers).toContain("user");
    expect(screen.getByText("ann")).toBeTruthy();
    expect(screen.queryByTestId("chart")).toBeNull();
  });
});
