import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation, useNavigate } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Explore from "./Explore";
import { encodeSpec } from "./exploreUrl";
import { widgetFromSpec } from "./widgetSpec";
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
  /** The project's widgets, as the list endpoint returns them. */
  widgets: [] as unknown[],
  /** Whether the widget list is still loading. */
  listPending: false,
  /** Every widget write, in call order. */
  writes: [] as { kind: string; request: Record<string, unknown> }[],
  /** Whether the viewer holds project:write on the project. */
  projectWrite: false,
  /** What the cards' filter bar holds. */
  pageContext: {} as Record<string, unknown>,
  /** What the Widgets tab last asked of the cards' filter bar. */
  pageFilterConfig: undefined as Record<string, unknown> | undefined,
}));

type Write = "create" | "update" | "duplicate" | "delete";

// Each write succeeds at once and is applied to the list, as the refetch
// after it would show.
function applyWrite(kind: Write, request: Record<string, unknown>): unknown {
  testState.writes.push({ kind, request });
  const now = new Date();
  const created = (body: Record<string, unknown>) => {
    const widget = {
      ...body,
      id: `created-${testState.writes.length}`,
      projectId: "project",
      organizationId: "org",
      createdByUserId: "member-1",
      createdAt: now,
      updatedAt: now,
    };
    testState.widgets = [widget, ...testState.widgets];
    return widget;
  };
  switch (kind) {
    case "create":
      return created(
        request.createWidgetRequestBody as Record<string, unknown>,
      );
    case "duplicate": {
      const { id } = request.duplicateWidgetRequestBody as { id: string };
      const source = testState.widgets.find(
        (widget) => (widget as { id: string }).id === id,
      ) as Record<string, unknown>;
      return created({ ...source, name: `${String(source.name)} (copy)` });
    }
    case "update": {
      const body = request.updateWidgetRequestBody as Record<string, unknown>;
      let result: unknown;
      testState.widgets = testState.widgets.map((widget) => {
        const current = widget as Record<string, unknown>;
        return current.id === body.id
          ? (result = { ...current, ...body, updatedAt: now })
          : widget;
      });
      return result;
    }
    case "delete":
      testState.widgets = testState.widgets.filter(
        (widget) => (widget as { id: string }).id !== request.id,
      );
      return undefined;
  }
}

function mockWrite(kind: Write) {
  return () => ({
    isPending: false,
    mutate: (
      { request }: { request: Record<string, unknown> },
      options?: { onSuccess?: (data: unknown) => unknown },
    ) => {
      void options?.onSuccess?.(applyWrite(kind, request));
    },
  });
}

vi.mock("@gram/client/react-query/widgets.js", () => ({
  useWidgets: () => ({
    isPending: testState.listPending,
    isFetching: testState.listPending,
    isError: false,
    data: testState.listPending ? undefined : { widgets: testState.widgets },
    refetch: vi.fn(),
  }),
  invalidateAllWidgets: () => Promise.resolve(),
}));
vi.mock("@gram/client/react-query/widget.js", () => ({
  invalidateAllWidget: () => Promise.resolve(),
}));
vi.mock("@gram/client/react-query/createWidget.js", () => ({
  useCreateWidgetMutation: mockWrite("create"),
}));
vi.mock("@gram/client/react-query/updateWidget.js", () => ({
  useUpdateWidgetMutation: mockWrite("update"),
}));
vi.mock("@gram/client/react-query/duplicateWidget.js", () => ({
  useDuplicateWidgetMutation: mockWrite("duplicate"),
}));
vi.mock("@gram/client/react-query/deleteWidget.js", () => ({
  useDeleteWidgetMutation: mockWrite("delete"),
}));
vi.mock("@/contexts/Auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/contexts/Auth")>()),
  useUser: () => ({ id: "member-1", email: "member@example.invalid" }),
  useProject: () => ({ id: "project", slug: "project" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, projectId: string) =>
      testState.projectWrite &&
      scope === "project:write" &&
      projectId === "project",
  }),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: {
      members: [
        {
          id: "member-1",
          name: "Test Member",
          email: "member@example.invalid",
        },
      ],
    },
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
// The Window control is the dashboard's date picker; its natural-language
// parsing needs a session and a server, which nothing here exercises.
vi.mock("@/components/DashboardTimeRangePicker", () => ({
  TimeRangePicker: ({
    preset,
    customRange,
    customRangeLabel,
    onPresetChange,
  }: {
    preset: string | null;
    customRange: { from: Date; to: Date } | null;
    customRangeLabel: string | null;
    onPresetChange: (preset: string) => void;
  }) => (
    <select
      aria-label="Window"
      value={customRange ? "custom" : (preset ?? "")}
      onChange={(event) => onPresetChange(event.target.value)}
    >
      {customRange ? (
        <option value="custom">
          {customRangeLabel ??
            `${customRange.from.toISOString()} – ${customRange.to.toISOString()}`}
        </option>
      ) : null}
      {["15m", "1h", "4h", "1d", "2d", "3d", "7d", "15d", "30d", "90d"].map(
        (value) => (
          <option key={value} value={value}>
            {value}
          </option>
        ),
      )}
    </select>
  ),
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
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
vi.mock("@/routes", () => ({
  useRoutes: () => ({ explore: { href: () => "/explore" } }),
}));
vi.mock("@/components/page-templates", () => ({
  WorkbenchPage: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/page-layout", async () => {
  const { Toolbar } = await import("@/components/ui/Toolbar");
  return { Page: { Eyebrow: () => null, Toolbar } };
});
// The cards' filter bar is the shared one, tested with usePageFilters.
vi.mock("./usePageFilters", () => ({
  usePageFilters: (config: Record<string, unknown>) => {
    testState.pageFilterConfig = config;
    return {
      toolbar: {
        schema: [],
        values: {},
        optionsById: {},
        onChange: () => {},
        onClear: () => {},
        onClearAll: () => {},
      },
      context: testState.pageContext,
    };
  },
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

// Cards mount as they scroll into view; here every card is in view.
class VisibleObserver {
  private readonly callback: IntersectionObserverCallback;
  constructor(callback: IntersectionObserverCallback) {
    this.callback = callback;
  }
  observe(target: Element) {
    this.callback(
      [{ isIntersecting: true, target } as IntersectionObserverEntry],
      this as unknown as IntersectionObserver,
    );
  }
  disconnect() {}
  unobserve() {}
  takeRecords() {
    return [];
  }
}
vi.stubGlobal("IntersectionObserver", VisibleObserver);

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
    testState.widgets = [];
    testState.listPending = false;
    testState.writes = [];
    testState.projectWrite = false;
    testState.pageContext = {};
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

  it("runs nothing until asked, then the one query the chart draws", () => {
    renderExplore();

    expect(screen.getByText("Nothing has run yet")).toBeTruthy();
    expect(testState.bodies.every((body) => body === null)).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Run query" }));
    expect(screen.queryByText("Nothing has run yet")).toBeNull();
    // The opening spec is a line chart, so one bucketed query runs over the
    // short default window, and no whole-window query beside it.
    const sent = testState.bodies.filter((body) => body !== null);
    expect(new Set(sent.map((body) => body?.grain))).toEqual(new Set(["hour"]));
    const chart = sent[0];
    expect(chart?.dataset).toBe("sessions");
    expect(chart?.grain).toBe("hour");
    expect(chart?.dimensions).toEqual(["user"]);
    expect(chart?.measures).toEqual([
      { op: "count", field: undefined, alias: "count" },
    ]);
    expect(chart?.limit).toBe(1000);
    expect(chart!.to.getTime() - chart!.from.getTime()).toBe(24 * 3_600_000);
  });

  it("asks for whole-window figures once the chart type is a table", () => {
    renderExplore();
    fireEvent.click(screen.getByRole("button", { name: "Table" }));
    testState.bodies = [];

    fireEvent.click(screen.getByRole("button", { name: "Run query" }));
    const sent = testState.bodies.filter((body) => body !== null);
    expect(sent.length).toBeGreaterThan(0);
    expect(sent.every((body) => body?.grain === "none")).toBe(true);
  });

  it("offers order and limit only on whole-window charts", () => {
    renderExplore();
    // The opening line chart is drawn in time order up to the server's cap.
    expect(screen.queryByRole("combobox", { name: "Order by" })).toBeNull();
    expect(screen.queryByRole("spinbutton", { name: "Limit" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Table" }));
    expect(screen.getByRole("combobox", { name: "Order by" })).toBeTruthy();
    expect(screen.getByRole("spinbutton", { name: "Limit" })).toBeTruthy();
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
      expect(
        (screen.getByRole("combobox", { name: "Window" }) as HTMLSelectElement)
          .value,
      ).toBe("7d");
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

    it("runs a linked absolute range, and will not save it as a widget", () => {
      const from = Date.UTC(2026, 8, 14, 10);
      const to = Date.UTC(2026, 8, 14, 12);
      renderExplore(linkTo({ ...toolCallsTable, range: { from, to } }));

      const asked = testState.bodies.find((body) => body !== null);
      expect(asked?.from.getTime()).toBe(from);
      expect(asked?.to.getTime()).toBe(to);
      expect(
        (screen.getByRole("combobox", { name: "Window" }) as HTMLSelectElement)
          .value,
      ).toBe("custom");
      const save = screen.getByRole("button", { name: "Save widget" });
      expect((save as HTMLButtonElement).disabled).toBe(true);
    });

    it("runs a linked query once a refreshed catalog can answer it", () => {
      testState.datasets = [sessions];
      const view = renderExplore(linkTo(toolCallsTable));
      expect(screen.getByText("Nothing has run yet")).toBeTruthy();

      testState.datasets = [sessions, toolCalls];
      view.rerender(
        <MemoryRouter initialEntries={["/before"]}>
          <RouterProbe />
          <TooltipProvider>
            <Explore />
          </TooltipProvider>
        </MemoryRouter>,
      );

      expect(
        screen.getByRole("combobox", { name: "Dataset" }).textContent,
      ).toBe("tool_calls");
      expect(screen.queryByText("Nothing has run yet")).toBeNull();
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

  describe("widgets", () => {
    const p95ByTool: ExploreSpec = {
      dataset: "tool_calls",
      measures: [{ op: "p95", field: "duration_ms" }],
      filters: [],
      dimensions: ["tool_name"],
      orderBy: "p95_duration_ms",
      limit: 0,
      window: "7d",
      chartType: "table",
    };

    function storedWidget(
      id: string,
      name: string,
      spec: ExploreSpec,
      extra: Record<string, unknown> = {},
    ) {
      return {
        id,
        name,
        dataset: spec.dataset,
        ...widgetFromSpec(spec),
        projectId: "project",
        organizationId: "org",
        createdByUserId: "member-1",
        createdAt: new Date(),
        updatedAt: new Date(),
        ...extra,
      };
    }

    function widgetsTab() {
      return screen.getByRole("tab", { name: /Widgets/ });
    }

    function showWidgets() {
      fireEvent.click(widgetsTab());
    }

    // Opens a widget from the list, as clicking its row does.
    function openWidget(name: string) {
      showWidgets();
      fireEvent.click(screen.getByText(name));
    }

    function param(name: string) {
      return new URLSearchParams(nav.search).get(name);
    }

    it("counts the project's widgets on the tab and lists who saved each", () => {
      testState.widgets = [
        storedWidget("w-1", "Slow tools", p95ByTool, {
          description: "Tools worth a look",
        }),
        storedWidget("w-2", "Slow tools", p95ByTool, {
          createdByUserId: "departed",
        }),
      ];
      renderExplore();

      expect(widgetsTab().textContent).toContain("2");
      showWidgets();
      expect(param("tab")).toBe("widgets");
      // Names repeat freely; the creator tells them apart.
      expect(screen.getAllByText("Slow tools")).toHaveLength(2);
      expect(screen.getByText("Tools worth a look")).toBeTruthy();
      expect(screen.getByText("Test Member")).toBeTruthy();
      expect(screen.getByText("A former member")).toBeTruthy();
      expect(screen.getAllByText("Table")).not.toHaveLength(0);
    });

    it("opens straight onto the list from a link", () => {
      testState.widgets = [storedWidget("w-1", "Slow tools", p95ByTool)];
      renderExplore("/explore?tab=widgets");

      expect(screen.getByText("Slow tools")).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Run query" })).toBeNull();
    });

    it("points an empty list back at Explore", () => {
      renderExplore();
      showWidgets();

      expect(screen.getByText("No widgets yet")).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Go to Explore" }));
      expect(param("tab")).toBeNull();
      expect(screen.getByRole("button", { name: "Run query" })).toBeTruthy();
    });

    it("narrows the list by name", async () => {
      testState.widgets = [
        storedWidget("w-1", "Slow tools", p95ByTool),
        storedWidget(
          "w-2",
          "Sessions by user",
          { ...p95ByTool, dataset: "sessions", measures: [], orderBy: "" },
          { createdByUserId: "someone-else" },
        ),
      ];
      renderExplore();
      showWidgets();

      fireEvent.change(screen.getByPlaceholderText("Search widgets"), {
        target: { value: "slow" },
      });
      // The toolbar's search applies on the next tick.
      await waitFor(() =>
        expect(screen.queryByText("Sessions by user")).toBeNull(),
      );
      expect(screen.getByText("Slow tools")).toBeTruthy();

      fireEvent.change(screen.getByPlaceholderText("Search widgets"), {
        target: { value: "nothing like it" },
      });
      expect(
        await screen.findByText("No widgets match these filters."),
      ).toBeTruthy();
    });

    it("opens a widget from the list in Explore, restored exactly, and runs it", () => {
      testState.widgets = [storedWidget("w-1", "Slow tools", p95ByTool)];
      renderExplore();
      openWidget("Slow tools");

      expect(param("tab")).toBeNull();
      expect(param("widget")).toBe("w-1");
      expect(
        screen.getByRole("combobox", { name: "Dataset" }).textContent,
      ).toBe("tool_calls");
      expect(
        (screen.getByRole("combobox", { name: "Window" }) as HTMLSelectElement)
          .value,
      ).toBe("7d");
      expect(urlSpec()).toMatchObject(p95ByTool);

      const summary = testState.bodies.findLast((body) => body !== null);
      expect(summary?.dataset).toBe("tool_calls");
      expect(summary?.orderBy).toEqual([
        { measure: "p95_duration_ms", direction: "desc" },
      ]);
      // Open, unedited: nothing to save.
      expect(screen.getByRole("button", { name: "Save" })).toHaveProperty(
        "disabled",
        true,
      );
    });

    it("draws each widget as a card on its own saved question, and keeps the view in the URL", () => {
      testState.widgets = [
        storedWidget("w-1", "Slow tools", p95ByTool),
        storedWidget("w-2", "Sessions by user", {
          ...p95ByTool,
          dataset: "sessions",
          measures: [{ op: "count", field: "" }],
          dimensions: ["user"],
          orderBy: "",
        }),
      ];
      renderExplore();
      showWidgets();
      testState.bodies = [];
      fireEvent.click(screen.getByRole("radio", { name: "Grid view" }));

      expect(param("view")).toBe("cards");
      expect(screen.getByRole("region", { name: "Slow tools" })).toBeTruthy();
      expect(
        screen.getByRole("region", { name: "Sessions by user" }),
      ).toBeTruthy();
      expect(screen.queryByRole("table")).toBeNull();
      const asked = testState.bodies
        .filter((body) => body !== null)
        .map((body) => body.dataset);
      expect(asked).toContain("tool_calls");
      expect(asked).toContain("sessions");
    });

    it("fetches the cards' filter options only in the cards view, from the widgets' datasets", () => {
      testState.widgets = [
        storedWidget("w-1", "Slow tools", { ...p95ByTool, window: "90d" }),
      ];
      renderExplore("/explore?tab=widgets");
      expect(testState.pageFilterConfig?.optionsEnabled).toBe(false);

      fireEvent.click(screen.getByRole("radio", { name: "Grid view" }));
      expect(testState.pageFilterConfig).toMatchObject({
        optionsEnabled: true,
        optionsDatasets: ["tool_calls"],
        optionsWindow: "90d",
      });
    });

    it("opens a card the filter bar narrowed as the question it ran, not the saved widget", () => {
      const byUser: ExploreSpec = {
        ...p95ByTool,
        dataset: "sessions",
        measures: [{ op: "count", field: "" }],
        dimensions: ["user"],
        orderBy: "",
      };
      testState.widgets = [storedWidget("w-1", "Sessions by user", byUser)];
      testState.pageContext = { filters: { user: ["ann"] } };
      renderExplore("/explore?tab=widgets&view=cards");

      const card = screen.getByRole("region", { name: "Sessions by user" });
      fireEvent.click(
        within(card).getByRole("button", { name: /Open in Explore/ }),
      );
      expect(param("widget")).toBeNull();
      expect(urlSpec()?.filters).toEqual([
        { field: "user", operator: "in", values: ["ann"] },
      ]);
    });

    it("opens a card's widget in the builder, and filters cards as it filters rows", async () => {
      testState.widgets = [
        storedWidget("w-1", "Slow tools", p95ByTool),
        storedWidget("w-2", "Other tools", p95ByTool),
      ];
      renderExplore("/explore?tab=widgets&view=cards");
      fireEvent.change(screen.getByPlaceholderText("Search widgets"), {
        target: { value: "slow" },
      });
      await waitFor(() =>
        expect(
          screen.queryByRole("region", { name: "Other tools" }),
        ).toBeNull(),
      );

      const card = screen.getByRole("region", { name: "Slow tools" });
      fireEvent.click(
        within(card).getByRole("button", { name: /Open in Explore/ }),
      );
      expect(param("tab")).toBeNull();
      expect(param("widget")).toBe("w-1");
      expect(urlSpec()).toMatchObject(p95ByTool);
    });

    it("saves the builder as a widget with a description, then has it open", () => {
      renderExplore();
      expect(screen.getByRole("img", { name: "Unsaved widget" })).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Table" }));
      fireEvent.click(screen.getByRole("button", { name: "Save widget" }));
      fireEvent.change(screen.getByRole("textbox", { name: "Widget name" }), {
        target: { value: "  Sessions by user  " },
      });
      fireEvent.change(
        screen.getByRole("textbox", { name: "Widget description" }),
        { target: { value: "Who runs the most sessions" } },
      );
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      const [write] = testState.writes;
      expect(write?.kind).toBe("create");
      expect(write?.request.createWidgetRequestBody).toEqual({
        name: "Sessions by user",
        description: "Who runs the most sessions",
        dataset: "sessions",
        query: {
          window: "1d",
          grain: "none",
          ungrouped: false,
          dimensions: ["user"],
          measures: [{ op: "count", field: "", alias: "count" }],
          filters: [],
          order_by: [],
          limit: 0,
        },
        visualization: { type: "table", options: {} },
      });
      expect(param("widget")).toBe("created-1");
      // With the widget open, the dataset row offers Save rather than
      // Save widget.
      expect(screen.getByRole("button", { name: "Save" })).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Save widget" })).toBeNull();
    });

    it("saves edits to the open widget in place", () => {
      testState.widgets = [storedWidget("w-1", "Slow tools", p95ByTool)];
      renderExplore();
      openWidget("Slow tools");
      expect(screen.queryByRole("img", { name: "Unsaved changes" })).toBeNull();

      fireEvent.click(screen.getByRole("button", { name: "Bar" }));
      expect(screen.getByRole("img", { name: "Unsaved changes" })).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      const [write] = testState.writes;
      expect(write?.kind).toBe("update");
      expect(write?.request.updateWidgetRequestBody).toMatchObject({
        id: "w-1",
        name: "Slow tools",
        dataset: "tool_calls",
        query: { grain: "day" },
        visualization: { type: "bar" },
      });
    });

    it("renames without saving the builder's edits", async () => {
      const user = userEvent.setup();
      const stored = storedWidget("w-1", "Slow tools", p95ByTool);
      testState.widgets = [stored];
      renderExplore();
      openWidget("Slow tools");
      fireEvent.click(screen.getByRole("button", { name: "Bar" }));

      await user.click(screen.getByRole("button", { name: "Widget actions" }));
      await user.click(screen.getByRole("menuitem", { name: /Rename/ }));
      const field = screen.getByRole("textbox", { name: "Widget name" });
      expect(field).toHaveProperty("value", "Slow tools");
      await user.clear(field);
      await user.type(field, "Slowest tools");
      await user.click(screen.getByRole("button", { name: "Rename" }));

      expect(testState.writes[0]?.request.updateWidgetRequestBody).toEqual({
        id: "w-1",
        name: "Slowest tools",
        description: undefined,
        dataset: "tool_calls",
        query: stored.query,
        visualization: stored.visualization,
      });
      expect(screen.getByRole("img", { name: "Unsaved changes" })).toBeTruthy();
    });

    it("duplicates the open widget and opens the copy", async () => {
      const user = userEvent.setup();
      testState.widgets = [
        storedWidget("w-1", "Slow tools", p95ByTool, {
          createdByUserId: "someone-else",
        }),
      ];
      renderExplore();
      openWidget("Slow tools");

      await user.click(screen.getByRole("button", { name: "Widget actions" }));
      await user.click(screen.getByRole("menuitem", { name: /Duplicate/ }));

      expect(testState.writes[0]).toEqual({
        kind: "duplicate",
        request: { duplicateWidgetRequestBody: { id: "w-1" } },
      });
      expect(param("widget")).toBe("created-1");
    });

    it("offers only copying on someone else's widget without project write", async () => {
      const user = userEvent.setup();
      testState.widgets = [
        storedWidget("w-1", "Slow tools", p95ByTool, {
          createdByUserId: "someone-else",
        }),
      ];
      renderExplore();
      openWidget("Slow tools");

      expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
      await user.click(screen.getByRole("button", { name: "Widget actions" }));
      expect(
        screen.getByRole("menuitem", { name: /Save as new widget/ }),
      ).toBeTruthy();
      expect(screen.getByRole("menuitem", { name: /Duplicate/ })).toBeTruthy();
      expect(screen.queryByRole("menuitem", { name: /Rename/ })).toBeNull();
      expect(screen.queryByRole("menuitem", { name: /Delete/ })).toBeNull();
      await user.keyboard("{Escape}");

      showWidgets();
      await user.click(
        screen.getByRole("button", { name: "Actions for Slow tools" }),
      );
      expect(screen.getByRole("menuitem", { name: /Open/ })).toBeTruthy();
      expect(screen.getByRole("menuitem", { name: /Duplicate/ })).toBeTruthy();
      expect(screen.queryByRole("menuitem", { name: /Rename/ })).toBeNull();
      expect(screen.queryByRole("menuitem", { name: /Delete/ })).toBeNull();
    });

    it("lets a project writer change someone else's widget", async () => {
      const user = userEvent.setup();
      testState.projectWrite = true;
      testState.widgets = [
        storedWidget("w-1", "Slow tools", p95ByTool, {
          createdByUserId: "someone-else",
        }),
      ];
      renderExplore();
      openWidget("Slow tools");

      expect(screen.getByRole("button", { name: "Save" })).toBeTruthy();
      await user.click(screen.getByRole("button", { name: "Widget actions" }));
      expect(screen.getByRole("menuitem", { name: /Rename/ })).toBeTruthy();
      expect(screen.getByRole("menuitem", { name: /Delete/ })).toBeTruthy();
    });

    it("duplicates a widget from the list and opens the copy in Explore", async () => {
      const user = userEvent.setup();
      testState.widgets = [storedWidget("w-1", "Slow tools", p95ByTool)];
      renderExplore();
      showWidgets();

      await user.click(
        screen.getByRole("button", { name: "Actions for Slow tools" }),
      );
      await user.click(screen.getByRole("menuitem", { name: /Duplicate/ }));

      expect(testState.writes[0]?.kind).toBe("duplicate");
      expect(param("tab")).toBeNull();
      expect(param("widget")).toBe("created-1");
    });

    it("deletes the open widget and keeps the builder", async () => {
      const user = userEvent.setup();
      testState.widgets = [storedWidget("w-1", "Slow tools", p95ByTool)];
      renderExplore();
      openWidget("Slow tools");

      await user.click(screen.getByRole("button", { name: "Widget actions" }));
      await user.click(screen.getByRole("menuitem", { name: /Delete/ }));
      await user.click(screen.getByRole("button", { name: "Delete" }));

      expect(testState.writes[0]).toEqual({
        kind: "delete",
        request: { id: "w-1" },
      });
      expect(param("widget")).toBeNull();
      expect(screen.getByRole("img", { name: "Unsaved widget" })).toBeTruthy();
      expect(
        screen.getByRole("combobox", { name: "Dataset" }).textContent,
      ).toBe("tool_calls");
    });

    it("opens a widget the catalog broke for editing, naming what is gone", () => {
      const reason = 'unknown_field: field "retired_field" does not exist';
      testState.widgets = [
        storedWidget(
          "w-1",
          "Old breakdown",
          { ...p95ByTool, dimensions: ["retired_field"] },
          { invalidReason: reason },
        ),
      ];
      renderExplore();
      showWidgets();
      expect(screen.getByLabelText("No longer works")).toBeTruthy();
      fireEvent.click(screen.getByText("Old breakdown"));

      expect(
        screen.getByText(/This widget no longer works/).textContent,
      ).toContain(reason);
      // The builder holds the saved widget, unrun, ready to be fixed.
      expect(
        screen.getByRole("combobox", { name: "Dataset" }).textContent,
      ).toBe("tool_calls");
      expect(screen.getByText("Nothing has run yet")).toBeTruthy();
      expect(testState.bodies.every((body) => body === null)).toBe(true);
    });

    it("offers no save as new while a linked widget is still loading", () => {
      testState.listPending = true;
      renderExplore(`${linkTo(p95ByTool)}&widget=w-1`);

      expect(
        screen.getByRole("button", { name: "Save widget" }),
      ).toHaveProperty("disabled", true);
    });

    it("names the problem when a widget cannot be read at all", () => {
      testState.widgets = [
        {
          ...storedWidget("w-1", "Unreadable", p95ByTool),
          query: { window: "1y" },
          invalidReason: 'window "1y" is not one of 1h, 24h, 7d, 30d, 90d',
        },
      ];
      renderExplore();
      openWidget("Unreadable");

      expect(
        screen.getByText(/can't be opened in the builder/).textContent,
      ).toContain('window "1y"');
      expect(screen.getByRole("combobox", { name: "Dataset" })).toBeTruthy();
      // The builder is not showing the widget, so saving would overwrite it.
      expect(screen.getByRole("button", { name: "Save" })).toHaveProperty(
        "disabled",
        true,
      );
      expect(screen.queryByRole("img", { name: "Unsaved changes" })).toBeNull();
    });

    it("refuses to save over a widget the builder cannot read though the server can", async () => {
      const user = userEvent.setup();
      const stored = storedWidget("w-1", "Ascending", p95ByTool);
      testState.widgets = [
        {
          ...stored,
          query: {
            ...stored.query,
            order_by: [{ measure: "p95_duration_ms", direction: "asc" }],
          },
        },
      ];
      renderExplore();
      openWidget("Ascending");

      expect(
        screen.getByText(/can't be opened in the builder/).textContent,
      ).toContain("options the builder doesn't offer");
      expect(screen.queryByText(/This widget no longer works/)).toBeNull();
      expect(screen.getByRole("button", { name: "Save" })).toHaveProperty(
        "disabled",
        true,
      );
      expect(screen.queryByRole("img", { name: "Unsaved changes" })).toBeNull();

      // Copying it, or saving the builder as a new one, is still offered.
      await user.click(screen.getByRole("button", { name: "Widget actions" }));
      expect(
        screen.getByRole("menuitem", { name: /Save as new widget/ }),
      ).toBeTruthy();
      expect(screen.getByRole("menuitem", { name: /Duplicate/ })).toBeTruthy();
      expect(screen.getByRole("menuitem", { name: /Delete/ })).toBeTruthy();
    });

    describe("leaving unsaved edits", () => {
      const sessionsByUser: ExploreSpec = {
        ...p95ByTool,
        dataset: "sessions",
        measures: [{ op: "count", field: "" }],
        dimensions: ["user"],
        orderBy: "",
      };

      it("asks before opening another widget over them, and Cancel keeps them", () => {
        testState.widgets = [
          storedWidget("w-1", "Slow tools", p95ByTool),
          storedWidget("w-2", "Sessions by user", sessionsByUser),
        ];
        renderExplore();
        openWidget("Slow tools");
        fireEvent.click(screen.getByRole("button", { name: "Bar" }));

        openWidget("Sessions by user");
        expect(
          screen.getByText(/Discard unsaved changes to “Slow tools”\?/),
        ).toBeTruthy();
        fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

        expect(param("widget")).toBe("w-1");
        expect(urlSpec()).toMatchObject({ ...p95ByTool, chartType: "bar" });

        fireEvent.click(screen.getByText("Sessions by user"));
        fireEvent.click(
          screen.getByRole("button", { name: "Discard and open" }),
        );
        expect(param("widget")).toBe("w-2");
        expect(urlSpec()).toMatchObject(sessionsByUser);
      });

      it("asks before duplicating from the list, and Cancel copies nothing", async () => {
        const user = userEvent.setup();
        testState.widgets = [
          storedWidget("w-1", "Slow tools", p95ByTool),
          storedWidget("w-2", "Sessions by user", sessionsByUser),
        ];
        renderExplore();
        openWidget("Slow tools");
        fireEvent.click(screen.getByRole("button", { name: "Bar" }));
        showWidgets();

        await user.click(
          screen.getByRole("button", { name: "Actions for Sessions by user" }),
        );
        await user.click(screen.getByRole("menuitem", { name: /Duplicate/ }));
        expect(
          screen.getByText(/Discard unsaved changes to “Slow tools”\?/),
        ).toBeTruthy();
        await user.click(screen.getByRole("button", { name: "Cancel" }));

        expect(testState.writes).toHaveLength(0);
        expect(param("widget")).toBe("w-1");
      });

      it("opens another widget straight away when nothing is unsaved", () => {
        testState.widgets = [
          storedWidget("w-1", "Slow tools", p95ByTool),
          storedWidget("w-2", "Sessions by user", sessionsByUser),
        ];
        renderExplore();
        openWidget("Slow tools");

        openWidget("Sessions by user");
        expect(screen.queryByText(/Discard unsaved changes/)).toBeNull();
        expect(param("widget")).toBe("w-2");
      });

      it("opens a widget straight away over a builder with no widget open", () => {
        testState.widgets = [storedWidget("w-1", "Slow tools", p95ByTool)];
        renderExplore();
        fireEvent.click(screen.getByRole("button", { name: "Bar" }));

        openWidget("Slow tools");
        expect(screen.queryByText(/Discard unsaved changes/)).toBeNull();
        expect(param("widget")).toBe("w-1");
      });

      it("asks before duplicating from the bar, and Cancel copies nothing", async () => {
        const user = userEvent.setup();
        testState.widgets = [storedWidget("w-1", "Slow tools", p95ByTool)];
        renderExplore();
        openWidget("Slow tools");
        fireEvent.click(screen.getByRole("button", { name: "Bar" }));

        await user.click(
          screen.getByRole("button", { name: "Widget actions" }),
        );
        await user.click(screen.getByRole("menuitem", { name: /Duplicate/ }));
        expect(
          screen.getByText(/Discard unsaved changes to “Slow tools”\?/),
        ).toBeTruthy();
        await user.click(screen.getByRole("button", { name: "Cancel" }));

        expect(testState.writes).toHaveLength(0);
        expect(param("widget")).toBe("w-1");
        expect(urlSpec()).toMatchObject({ chartType: "bar" });
      });
    });
  });
});
