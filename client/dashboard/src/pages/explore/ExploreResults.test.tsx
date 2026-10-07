import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { specForDataset, type ExploreSpec } from "./exploreModel";
import { ExploreResults, type RunQuery } from "./ExploreResults";
import { MAX_SERIES } from "./resultSeries";

// The chart is drawn to a canvas, so the mock exposes what the drawing was
// told: the series, their dataset types and fills, and whether the scales
// stack.
vi.mock("react-chartjs-2", () => ({
  Chart: ({
    type,
    data,
    options,
  }: {
    type: string;
    data: {
      datasets: { label: string; type: string; fill?: string | boolean }[];
    };
    options: { scales: { y: { stacked?: boolean } } };
  }) => (
    <div
      data-testid="chart"
      data-type={type}
      data-stacked={String(options.scales.y.stacked ?? false)}
      data-dataset-types={data.datasets
        .map((dataset) => dataset.type)
        .join(",")}
      data-fills={data.datasets
        .map((dataset) => String(dataset.fill ?? ""))
        .join(",")}
    >
      {data.datasets.map((dataset) => dataset.label).join(",")}
    </div>
  ),
}));
vi.mock("@/lib/theme", () => ({ useIsDarkTheme: () => false }));
vi.mock("@/components/chart/useSeriesColors", () => ({
  useSeriesColors: () => ["#000", "#111"],
  useOtherSeriesColor: () => "#888",
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
        result={query({ isFetching: true })}
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
        result={loaded([{ user: "ann", count: 1250 }])}
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
        result={query({
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
        result={loaded([])}
      />,
    );
    expect(screen.getByText("No rows to show")).toBeTruthy();
  });

  it("tables a result with a column per dimension and measure, values formatted by unit", () => {
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
        result={loaded([
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
        result={loaded([{ count: 12500 }])}
      />,
    );
    expect(screen.getByText("COUNT")).toBeTruthy();
    expect(screen.getByText("12.5K")).toBeTruthy();
  });

  it("draws a timeseries as its chart alone, with no table beneath", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "line" })}
        result={loaded([
          { time_bucket: "2026-09-14T10:00:00Z", user: "ann", count: 2 },
          { time_bucket: "2026-09-14T10:00:00Z", user: "bob", count: 1 },
        ])}
      />,
    );
    expect(screen.getByTestId("chart").textContent).toBe("ann,bob");
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("stacks a breakdown as bars on stacked scales", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "stacked_bar" })}
        result={loaded([
          { time_bucket: "2026-09-14T10:00:00Z", user: "ann", count: 2 },
          { time_bucket: "2026-09-14T10:00:00Z", user: "bob", count: 1 },
        ])}
      />,
    );
    const chart = screen.getByTestId("chart");
    expect(chart.textContent).toBe("ann,bob");
    expect(chart.dataset.type).toBe("bar");
    expect(chart.dataset.stacked).toBe("true");
    expect(chart.dataset.datasetTypes).toBe("bar,bar");
    expect(screen.queryByText(/drawn as/)).toBeNull();
  });

  it("stacks a breakdown as bands that fill down to the band below", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "stacked_area" })}
        result={loaded([
          { time_bucket: "2026-09-14T10:00:00Z", user: "ann", count: 2 },
          { time_bucket: "2026-09-14T10:00:00Z", user: "bob", count: 1 },
        ])}
      />,
    );
    const chart = screen.getByTestId("chart");
    expect(chart.dataset.type).toBe("line");
    expect(chart.dataset.stacked).toBe("true");
    expect(chart.dataset.datasetTypes).toBe("line,line");
    expect(chart.dataset.fills).toBe("stack,stack");
  });

  it("draws a stack with nothing to stack by as the plain chart it is, and says so", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "stacked_bar", dimensions: [] })}
        result={loaded([{ time_bucket: "2026-09-14T10:00:00Z", count: 3 }])}
      />,
    );
    const chart = screen.getByTestId("chart");
    expect(chart.dataset.type).toBe("bar");
    expect(chart.dataset.stacked).toBe("false");
    expect(
      screen.getByText(
        "Stacking needs a Group by, so this is drawn as a bar chart.",
      ),
    ).toBeTruthy();
  });

  it("folds the series past the cap into Other on a stack, and says how many", () => {
    const rows = Array.from({ length: MAX_SERIES + 3 }, (_, i) => ({
      time_bucket: "2026-09-14T10:00:00Z",
      user: `user-${i}`,
      count: i + 1,
    }));
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "stacked_bar" })}
        result={loaded(rows)}
      />,
    );
    const labels = screen.getByTestId("chart").textContent?.split(",") ?? [];
    expect(labels).toHaveLength(MAX_SERIES);
    expect(labels.at(-1)).toBe("Other");
    expect(
      screen.getByText(
        `Showing the ${MAX_SERIES - 1} largest of ${MAX_SERIES + 3} series; the other 4 are stacked as Other.`,
      ),
    ).toBeTruthy();
  });

  it("drops the series past the cap on a line, and says how many", () => {
    const rows = Array.from({ length: MAX_SERIES + 3 }, (_, i) => ({
      time_bucket: "2026-09-14T10:00:00Z",
      user: `user-${i}`,
      count: i + 1,
    }));
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "line" })}
        result={loaded(rows)}
      />,
    );
    expect(screen.getByTestId("chart").textContent?.split(",")).not.toContain(
      "Other",
    );
    expect(
      screen.getByText(
        `Showing the ${MAX_SERIES} largest of ${MAX_SERIES + 3} series.`,
      ),
    ).toBeTruthy();
  });

  it("refuses to stack more than one measure", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({
          chartType: "stacked_bar",
          measures: [
            { op: "count", field: "" },
            { op: "sum", field: "count" },
          ],
        })}
        result={loaded([{ time_bucket: "t", user: "ann", count: 1 }])}
      />,
    );
    expect(screen.getByText("A stacked chart stacks one measure")).toBeTruthy();
    expect(screen.queryByTestId("chart")).toBeNull();
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
        result={loaded([
          { time_bucket: "t", count: 1, sum_duration_seconds: 2 },
        ])}
      />,
    );
    expect(screen.getByText("These measures do not share a unit")).toBeTruthy();
    expect(screen.queryByTestId("chart")).toBeNull();
  });

  it("ranks the groups as horizontal bars, largest first, by the ordered measure", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({
          chartType: "ranked",
          measures: [
            { op: "count", field: "" },
            { op: "sum", field: "duration_seconds" },
          ],
          orderBy: "sum_duration_seconds",
        })}
        result={loaded([
          { user: "ann", count: 9, sum_duration_seconds: 10 },
          { user: "bob", count: 1, sum_duration_seconds: 40 },
        ])}
      />,
    );
    expect(screen.getByText("Ranked by SUM(duration_seconds)")).toBeTruthy();
    const labels = screen
      .getAllByRole("listitem")
      .map((item) => item.textContent);
    expect(labels).toEqual(["bob40", "ann10"]);
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("tables projected rows rather than ranking them when nothing is measured", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "ranked", measures: [] })}
        result={loaded([{ user: "ann", time: "2026-09-14T10:15:30Z" }])}
      />,
    );
    expect(screen.getByRole("table")).toBeTruthy();
    expect(screen.queryByRole("listitem")).toBeNull();
  });

  it("tables projected rows, time first, when nothing is measured", () => {
    render(
      <ExploreResults
        dataset={dataset}
        spec={spec({ chartType: "line", measures: [] })}
        result={loaded([{ user: "ann", time: "2026-09-14T10:15:30Z" }])}
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
