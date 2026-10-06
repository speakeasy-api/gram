import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { specForDataset, type ExploreSpec } from "./exploreModel";
import { decodeSpec, QUERY_PARAM, WIDGET_PARAM } from "./exploreUrl";
import { widgetFromSpec } from "./widgetSpec";
import { WidgetView, type ViewableWidget } from "./WidgetView";

const testState = vi.hoisted(() => ({
  datasets: undefined as unknown[] | undefined,
  describeError: false,
  /** Every body handed to the query hook; null is "nothing to run". */
  bodies: [] as (AnalyticsQueryPayload | null)[],
  rows: undefined as Record<string, unknown>[] | undefined,
  error: null as Error | null,
}));

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
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    explore: { href: () => "/acme/projects/default/explore" },
  }),
}));
vi.mock("@gram/client/react-query/analyticsDescribe.js", () => ({
  useAnalyticsDescribe: () => ({
    isError: testState.describeError,
    data:
      testState.datasets === undefined
        ? undefined
        : { datasets: testState.datasets },
  }),
}));
vi.mock("./useRunQuery", () => ({
  useRunQuery: (body: AnalyticsQueryPayload | null) => {
    testState.bodies.push(body);
    if (testState.error) {
      return { data: undefined, error: testState.error, isError: true };
    }
    return {
      data:
        testState.rows === undefined || body === null
          ? undefined
          : { dataset: body.dataset, plan: "", rows: testState.rows },
      error: null,
      isError: false,
    };
  },
}));

const sessions: AnalyticsDataset = {
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
      name: "cost_usd",
      type: "float64",
      role: "measure",
      default: false,
      unit: "usd",
      aggregations: ["sum"],
    },
  ],
};

function widget(
  overrides: Partial<ExploreSpec> = {},
  extra: Partial<ViewableWidget> = {},
): ViewableWidget {
  const spec = { ...specForDataset(sessions), ...overrides };
  return {
    name: "Sessions by user",
    dataset: spec.dataset,
    ...widgetFromSpec(spec),
    ...extra,
  };
}

function renderView(view: ViewableWidget) {
  return render(
    <MemoryRouter>
      <WidgetView widget={view} />
    </MemoryRouter>,
  );
}

describe("WidgetView", () => {
  beforeEach(() => {
    testState.datasets = [sessions];
    testState.describeError = false;
    testState.bodies = [];
    testState.rows = undefined;
    testState.error = null;
  });
  afterEach(() => {
    cleanup();
  });

  it("runs the widget's own query and draws it under the widget's name", () => {
    testState.rows = [
      { time_bucket: "2026-09-14T10:00:00Z", user: "ann", count: 2 },
      { time_bucket: "2026-09-14T10:00:00Z", user: "bob", count: 1 },
    ];
    renderView(widget({ chartType: "line" }));
    expect(screen.getByRole("heading", { name: "Sessions by user" }));
    expect(screen.getByTestId("chart").textContent).toBe("ann,bob");
    const body = testState.bodies.at(-1);
    expect(body?.dataset).toBe("sessions");
    expect(body?.grain).toBe("hour");
    expect(body?.dimensions).toEqual(["user"]);
  });

  it("draws a single figure bare, formatted by its unit", () => {
    testState.rows = [{ sum_cost_usd: 12.5 }];
    renderView(
      widget({
        chartType: "number",
        measures: [{ op: "sum", field: "cost_usd" }],
      }),
    );
    expect(screen.getByText("$12.50")).toBeTruthy();
    // The card already names the figure; no tile label repeats it.
    expect(screen.queryByText("SUM(cost_usd)")).toBeNull();
  });

  it("opens exactly the widget's query in Explore, and the saved widget when it has one", () => {
    const view = widget({ chartType: "table" }, { id: "widget-1" });
    renderView(view);
    const link = screen.getByRole("link", { name: /Open in Explore/ });
    const url = new URL(link.getAttribute("href") ?? "", "https://x.invalid");
    expect(url.pathname).toBe("/acme/projects/default/explore");
    expect(url.searchParams.get(WIDGET_PARAM)).toBe("widget-1");
    const opened = decodeSpec(url.searchParams.get(QUERY_PARAM), [sessions]);
    expect(opened && widgetFromSpec(opened)).toEqual(
      widgetFromSpec({
        ...specForDataset(sessions),
        chartType: "table",
      }),
    );
  });

  it("lets a number tile grow to fit a failure, and keeps a chart's height fixed", () => {
    renderView(
      widget(
        { chartType: "number" },
        { name: "Tile", invalidReason: "a long reason" },
      ),
    );
    const tile = screen.getByRole("alert").parentElement!;
    expect(tile.style.minHeight).toBe("72px");
    expect(tile.style.height).toBe("");
    cleanup();

    testState.rows = [];
    renderView(widget({ chartType: "line" }));
    const body = screen
      .getByText("Nothing in this window")
      .closest("[style]") as HTMLElement;
    expect(body.style.height).toBe("240px");
  });

  it("says why when the server reports the widget broken, and runs nothing", () => {
    renderView(
      widget({}, { invalidReason: 'field "user" is not a dimension' }),
    );
    expect(screen.getByRole("alert").textContent).toContain(
      'This widget no longer works: field "user" is not a dimension',
    );
    expect(testState.bodies).toEqual([]);
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("says why when the catalog no longer has what it asks, and runs nothing", () => {
    renderView(widget({ dimensions: ["team"] }));
    expect(screen.getByRole("alert").textContent).toContain(
      'field "team" is not a dimension of sessions',
    );
    // Unsaved, it would open Explore on nothing; there is no way in.
    expect(screen.queryByRole("link")).toBeNull();
    expect(testState.bodies.every((body) => body === null)).toBe(true);
  });

  it("still opens a saved widget that no longer works, where it is fixed", () => {
    renderView(widget({ dimensions: ["team"] }, { id: "widget-1" }));
    const link = screen.getByRole("link", { name: /Open in Explore/ });
    const url = new URL(link.getAttribute("href") ?? "", "https://x.invalid");
    expect(url.searchParams.get(WIDGET_PARAM)).toBe("widget-1");
  });

  it("says why when the builder cannot read the widget, with no way into Explore", () => {
    renderView({
      name: "Odd",
      dataset: "sessions",
      query: { window: "2h" },
      visualization: { type: "line" },
    });
    expect(screen.getByRole("alert").textContent).toContain(
      "options the builder doesn't offer",
    );
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("shows the server's reason when the query fails", () => {
    testState.error = new Error("query timed out");
    renderView(widget({ chartType: "table" }));
    expect(screen.getByRole("alert").textContent).toContain(
      "The query did not run: query timed out",
    );
  });

  it("waits for the catalog before running anything", () => {
    testState.datasets = undefined;
    renderView(widget());
    expect(screen.queryByRole("alert")).toBeNull();
    expect(testState.bodies.every((body) => body === null)).toBe(true);
  });

  it("says so when nothing in the window matches", () => {
    testState.rows = [];
    renderView(widget({ chartType: "table" }));
    expect(screen.getByText("Nothing in this window")).toBeTruthy();
  });
});
