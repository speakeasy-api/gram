import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation, useNavigate } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Explore from "./Explore";
import { encodeSpec } from "./exploreUrl";
import type { ExploreSpec } from "./exploreModel";

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
  /** Whether a query that runs comes back answered, rather than pending. */
  answers: false,
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
    if (testState.answers && body !== null) {
      return {
        data: { dataset: body.dataset, plan: "", rows: [] },
        error: null,
        isError: false,
        isSuccess: true,
        isPending: false,
        isFetching: false,
        isPlaceholderData: false,
      };
    }
    return {
      data: undefined,
      error: null,
      isError: false,
      isSuccess: false,
      isPending: true,
      isFetching: body !== null,
      isPlaceholderData: false,
    };
  },
}));
// A restored filter shows its value picker, which asks for the dimension's
// values; nothing here is about those, so the picker gets none.
vi.mock("./useDimensionValues", () => ({
  DIMENSION_VALUES_LIMIT: 200,
  useDimensionValues: () => ({
    data: undefined,
    error: null,
    isError: false,
    isPending: true,
    isFetching: false,
  }),
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
  fields: [
    {
      name: "user",
      type: "string",
      role: "dimension",
      default: true,
      operators: ["equals", "in"],
    },
    {
      name: "surface",
      type: "string",
      role: "dimension",
      default: false,
      operators: ["in"],
    },
    {
      name: "turn_count",
      type: "int64",
      role: "measure",
      default: false,
      aggregations: ["sum", "avg"],
    },
  ],
};

const toolCalls: AnalyticsDataset = {
  name: "tool_calls",
  kind: "event",
  grain: "tool call",
  description: "One row per tool call.",
  fields: [
    {
      name: "tool_name",
      type: "string",
      role: "dimension",
      default: true,
      operators: ["in"],
    },
    {
      name: "duration_ms",
      type: "float64",
      role: "measure",
      default: false,
      unit: "ms",
      aggregations: ["p95"],
    },
  ],
};

/** Where the router is, and a way to step back through its history. */
const nav = { pathname: "", search: "", back: () => {} };

function RouterProbe(): null {
  const location = useLocation();
  const navigate = useNavigate();
  nav.pathname = location.pathname;
  nav.search = location.search;
  nav.back = () => void navigate(-1);
  return null;
}

/** The query the router's URL currently carries. */
function urlSpec(): Partial<ExploreSpec> | null {
  const raw = new URLSearchParams(nav.search).get("q");
  return raw === null ? null : (JSON.parse(raw) as Partial<ExploreSpec>);
}

function linkTo(spec: ExploreSpec): string {
  return `/explore?${new URLSearchParams({ q: encodeSpec(spec) }).toString()}`;
}

// The dataset picker carries a tooltip, which needs the provider the app
// mounts above every page.
// A page visited before Explore sits behind it, so Back has somewhere to go.
function renderExplore(entry = "/explore") {
  return render(
    <MemoryRouter initialEntries={["/before", entry]} initialIndex={1}>
      <RouterProbe />
      <TooltipProvider>
        <Explore />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

describe("Explore", () => {
  beforeEach(() => {
    testState.isPending = false;
    testState.flagStatus = "enabled";
    testState.isError = false;
    testState.datasets = [sessions, toolCalls];
    testState.refetch.mockClear();
    testState.bodies = [];
    testState.answers = false;
  });

  afterEach(() => {
    cleanup();
  });

  it("opens on the catalog's first dataset with its defaults", () => {
    renderExplore();

    expect(screen.getByRole("heading", { name: "Explore" })).toBeTruthy();
    expect(screen.getByText("preview")).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "Dataset" }).textContent).toBe(
      "sessions",
    );
    // The description and grain live behind the info icon, not in the row.
    expect(screen.queryByText("One row per agent session.")).toBeNull();
    expect(
      screen.getByRole("button", { name: "About this dataset" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("combobox", { name: "Aggregation" }).textContent,
    ).toBe("count");
    expect(screen.getByText("of all rows")).toBeTruthy();
    // The summary field is the opening breakdown.
    expect(screen.getByText("user")).toBeTruthy();
  });

  it("runs nothing until asked, then both shapes of the query", () => {
    renderExplore();

    expect(screen.getByText("Nothing has run yet")).toBeTruthy();
    expect(testState.bodies.every((body) => body === null)).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Run query" }));
    expect(screen.queryByText("Nothing has run yet")).toBeNull();
    // The opening spec is a line chart, so a bucketed chart query and a
    // whole-window summary query both run, over the short default window.
    const chart = testState.bodies.find((body) => body?.grain === "hour");
    const summary = testState.bodies.find((body) => body?.grain === "none");
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
    renderExplore();
    fireEvent.click(screen.getByRole("button", { name: "Table" }));
    testState.bodies = [];

    fireEvent.click(screen.getByRole("button", { name: "Run query" }));
    const [chart, summary] = testState.bodies;
    expect(chart).toBeNull();
    expect(summary?.grain).toBe("none");
  });

  it("keeps the last run's results while the builder moves on", () => {
    renderExplore();
    fireEvent.click(screen.getByRole("button", { name: "Run query" }));
    testState.bodies = [];

    // An edit after a run changes nothing about what is queried until the
    // next Run; the builder only says it has moved on.
    expect(screen.queryByText("Changed since the last run.")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Table" }));
    expect(screen.getByText("Changed since the last run.")).toBeTruthy();
    expect(
      testState.bodies.every(
        (body) => body?.grain === "hour" || body?.grain === "none",
      ),
    ).toBe(true);
    expect(testState.bodies.some((body) => body?.grain === "hour")).toBe(true);
  });

  it("adds and removes filter rows", () => {
    renderExplore();

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
    renderExplore();

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
    fireEvent.click(screen.getByRole("button", { name: "Run query" }));
    const last = testState.bodies.at(-1);
    expect(last?.ungrouped).toBe(true);
    expect(last?.measures).toBeUndefined();
  });

  it("shows the catalog loading", () => {
    testState.isPending = true;
    renderExplore();

    expect(screen.getByLabelText("Loading the catalog")).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "Dataset" })).toBeNull();
  });

  it("offers a retry when the catalog fails to load", () => {
    testState.isError = true;
    renderExplore();

    expect(screen.getByText("The catalog did not load")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(testState.refetch).toHaveBeenCalledTimes(1);
  });

  it("says so when the catalog has no datasets, and runs nothing", () => {
    testState.datasets = [];
    renderExplore();

    expect(screen.getByText("No datasets yet")).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "Dataset" })).toBeNull();
    expect(testState.bodies.every((body) => body === null)).toBe(true);
  });

  it("stays closed to an organization the rollout has not reached", () => {
    testState.flagStatus = "disabled";
    testState.describeCalls = 0;
    renderExplore();

    expect(screen.getByText("Explore is not available yet")).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "Dataset" })).toBeNull();
    expect(testState.describeCalls, "it never asks for the catalog").toBe(0);
  });

  it("waits rather than saying no while the flag is still loading", () => {
    testState.flagStatus = "loading";
    renderExplore();

    expect(screen.getByLabelText("Loading the catalog")).toBeTruthy();
    expect(screen.queryByText("Explore is not available yet")).toBeNull();
  });

  describe("the URL", () => {
    const toolCallsTable: ExploreSpec = {
      dataset: "tool_calls",
      measures: [{ op: "p95", field: "duration_ms" }],
      filters: [{ field: "tool_name", operator: "in", values: ["a, b", "c"] }],
      dimensions: ["tool_name"],
      orderBy: "p95_duration_ms",
      limit: 25,
      window: "7d",
      chartType: "table",
    };

    it("carries every edit, replacing the entry while nothing has run", () => {
      renderExplore();

      fireEvent.click(screen.getByRole("button", { name: "Table" }));
      expect(urlSpec()?.chartType).toBe("table");
      fireEvent.click(screen.getByRole("button", { name: "Bar" }));
      expect(urlSpec()?.chartType).toBe("bar");

      // Neither edit left an entry behind: Back leaves Explore.
      act(() => nav.back());
      expect(nav.pathname).toBe("/before");
    });

    it("keeps each question that ran as a step Back returns to", () => {
      testState.answers = true;
      renderExplore();

      fireEvent.click(screen.getByRole("button", { name: "Run query" }));
      expect(urlSpec()?.chartType).toBe("line");

      // Edits after a run start a new entry, then keep replacing it.
      fireEvent.click(screen.getByRole("button", { name: "Table" }));
      fireEvent.click(screen.getByRole("button", { name: "Bar" }));
      expect(urlSpec()?.chartType).toBe("bar");

      testState.bodies = [];
      act(() => nav.back());
      expect(nav.pathname).toBe("/explore");
      expect(urlSpec()?.chartType).toBe("line");
      expect(screen.queryByText("Changed since the last run.")).toBeNull();

      act(() => nav.back());
      expect(nav.pathname).toBe("/before");
    });

    it("leaves a question that has not come back out of the history", () => {
      renderExplore();

      // The run never returns, so the entry stays a draft and is replaced.
      fireEvent.click(screen.getByRole("button", { name: "Run query" }));
      fireEvent.click(screen.getByRole("button", { name: "Table" }));

      act(() => nav.back());
      expect(nav.pathname).toBe("/before");
    });

    it("restores a linked query into the builder and runs it", () => {
      renderExplore(linkTo(toolCallsTable));

      expect(
        screen.getByRole("combobox", { name: "Dataset" }).textContent,
      ).toBe("tool_calls");
      expect(screen.getByRole("combobox", { name: "Window" }).textContent).toBe(
        "Last 7 days",
      );
      expect(screen.queryByText("Nothing has run yet")).toBeNull();

      // A table needs only the summary shape, asked exactly as linked.
      const summary = testState.bodies.find((body) => body !== null);
      expect(summary?.dataset).toBe("tool_calls");
      expect(summary?.grain).toBe("none");
      expect(summary?.dimensions).toEqual(["tool_name"]);
      expect(summary?.filters).toEqual([
        { field: "tool_name", operator: "in", values: ["a, b", "c"] },
      ]);
      expect(summary?.orderBy).toEqual([
        { measure: "p95_duration_ms", direction: "desc" },
      ]);
      expect(summary?.limit).toBe(25);
      expect(summary!.to.getTime() - summary!.from.getTime()).toBe(
        7 * 86_400_000,
      );
    });

    it.each([
      ["text that is not JSON", "/explore?q=%7Bnot-json"],
      [
        "a dataset the catalog dropped",
        linkTo({ ...toolCallsTable, dataset: "retired" }),
      ],
      [
        "a field the catalog dropped",
        linkTo({ ...toolCallsTable, dimensions: ["retired_field"] }),
      ],
      [
        "an older encoding",
        `/explore?${new URLSearchParams({ q: JSON.stringify({ v: 0 }) }).toString()}`,
      ],
    ])("opens the default view for %s", (_, entry) => {
      renderExplore(entry);

      expect(
        screen.getByRole("combobox", { name: "Dataset" }).textContent,
      ).toBe("sessions");
      expect(screen.getByText("Nothing has run yet")).toBeTruthy();
      expect(testState.bodies.every((body) => body === null)).toBe(true);
    });
  });
});
