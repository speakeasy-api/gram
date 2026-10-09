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
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  BuiltInDashboardRoute,
  DashboardRoute,
  DashboardsIndex,
  DashboardsRoot,
} from "./Dashboards";
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
  /** Whether the widget list failed to load. */
  listFailed: false,
  /** Every widget write, in call order. */
  writes: [] as { kind: string; request: Record<string, unknown> }[],
  /** Whether the viewer holds project:write on the project. */
  projectWrite: false,
  /** What the cards' filter bar holds. */
  pageContext: {} as Record<string, unknown>,
  /** What the Widgets tab last asked of the cards' filter bar. */
  pageFilterConfig: undefined as Record<string, unknown> | undefined,
  /** Whether the URL holds values for the page's filter bar. */
  pageTouched: false,
  /** Every whole-bar setting, in call order. */
  pageApplied: [] as unknown[],
  /** The project's dashboards, as the list endpoint returns them. */
  dashboards: [] as Record<string, unknown>[],
  /** The Speakeasy-built dashboards, as the list endpoint returns them. */
  builtIn: [] as Record<string, unknown>[],
  /** Every dashboard write, in call order. */
  dashboardWrites: [] as { kind: string; request: Record<string, unknown> }[],
  /** Who is told when the dashboards change, as react-query would tell. */
  dashboardListeners: new Set<() => void>(),
  /** Whether the dashboards list is still loading, or failed. */
  dashboardsPending: false,
  dashboardsFailed: false,
  /** Every success toast, with its text and action. */
  toasts: [] as {
    text: string;
    action?: { label: string; onClick: () => void };
  }[],
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
      // A new widget, like a copy, is on no dashboard yet.
      dashboards: [],
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
    isError: testState.listFailed,
    data:
      testState.listPending || testState.listFailed
        ? undefined
        : { widgets: testState.widgets },
    refetch: () => {
      testState.listFailed = false;
      return Promise.resolve();
    },
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

type DashboardWrite =
  | "create"
  | "update"
  | "saveLayout"
  | "duplicateBuiltIn"
  | "saveFilters"
  | "addWidget"
  | "removeWidget"
  | "duplicate"
  | "delete";

// Each dashboard write succeeds at once and is applied to the list, as the
// refetch after it would show.
function applyDashboardWrite(
  kind: DashboardWrite,
  request: Record<string, unknown>,
): unknown {
  testState.dashboardWrites.push({ kind, request });
  const now = new Date();
  const find = (id: unknown) =>
    testState.dashboards.find((dashboard) => dashboard.id === id)!;
  const put = (next: Record<string, unknown>) => {
    testState.dashboards = testState.dashboards.map((dashboard) =>
      dashboard.id === next.id ? next : dashboard,
    );
    return next;
  };
  const add = (dashboard: Record<string, unknown>) => {
    testState.dashboards = [dashboard, ...testState.dashboards];
    return dashboard;
  };
  const placements = (dashboard: Record<string, unknown>) =>
    dashboard.widgets as Record<string, unknown>[];
  switch (kind) {
    case "create":
      return add({
        ...(request.createDashboardRequestBody as Record<string, unknown>),
        id: `dashboard-${testState.dashboardWrites.length}`,
        projectId: "project",
        organizationId: "org",
        createdByUserId: "member-1",
        filters: { values: {} },
        widgets: [],
        createdAt: now,
        updatedAt: now,
      });
    case "update": {
      const body = request.updateDashboardRequestBody as { id: string };
      return put({ ...find(body.id), ...body, updatedAt: now });
    }
    case "saveLayout": {
      const body = request.saveDashboardLayoutRequestBody as {
        id: string;
        placements: Record<string, unknown>[];
      };
      return put({
        ...find(body.id),
        widgets: body.placements.map((placement, index) => ({
          id: placement.id ?? `placement-${index}`,
          ...placement,
        })),
        updatedAt: now,
      });
    }
    case "saveFilters": {
      const body = request.saveDashboardFiltersRequestBody as {
        id: string;
        filters: unknown;
      };
      return put({ ...find(body.id), filters: body.filters, updatedAt: now });
    }
    case "addWidget": {
      const body = request.addDashboardWidgetRequestBody as {
        id: string;
        widgetId: string;
      };
      const dashboard = find(body.id);
      const cards = placements(dashboard);
      return put({
        ...dashboard,
        widgets: [
          ...cards,
          {
            id: `placement-${cards.length + 1}`,
            widgetId: body.widgetId,
            x: 0,
            y: 99,
            w: 6,
            h: 3,
          },
        ],
        updatedAt: now,
      });
    }
    case "removeWidget": {
      const body = request.removeDashboardWidgetRequestBody as {
        id: string;
        placementId: string;
      };
      const dashboard = find(body.id);
      return put({
        ...dashboard,
        widgets: placements(dashboard).filter(
          (placement) => placement.id !== body.placementId,
        ),
        updatedAt: now,
      });
    }
    case "duplicate": {
      const { id } = request.duplicateDashboardRequestBody as { id: string };
      const source = find(id);
      return add({
        ...source,
        id: `dashboard-${testState.dashboardWrites.length}`,
        name: `${String(source.name)} (copy)`,
        createdByUserId: "member-1",
      });
    }
    case "duplicateBuiltIn": {
      const { slug } = request.duplicateBuiltInDashboardRequestBody as {
        slug: string;
      };
      const page = testState.builtIn.find(
        (candidate) => candidate.slug === slug,
      )!;
      return add({
        id: `dashboard-${testState.dashboardWrites.length}`,
        name: `${String(page.name)} (copy)`,
        description: page.description,
        projectId: "project",
        organizationId: "org",
        createdByUserId: "member-1",
        filters: { values: {} },
        widgets: [],
        createdAt: now,
        updatedAt: now,
      });
    }
    case "delete":
      testState.dashboards = testState.dashboards.filter(
        (dashboard) => dashboard.id !== request.id,
      );
      return undefined;
  }
}

function mockDashboardWrite(kind: DashboardWrite) {
  return () => ({
    isPending: false,
    mutate: (
      { request }: { request: Record<string, unknown> },
      options?: {
        onSuccess?: (data: unknown) => unknown;
        onSettled?: () => unknown;
      },
    ) => {
      void options?.onSuccess?.(applyDashboardWrite(kind, request));
      void options?.onSettled?.();
      // The refetch after a write lands at once.
      for (const listener of testState.dashboardListeners) listener();
    },
  });
}

/** Re-renders the caller when the dashboards change, as react-query would. */
async function dashboardSubscription() {
  const { useEffect, useReducer } = await import("react");
  return function useDashboardChanges() {
    const [, bump] = useReducer((n: number) => n + 1, 0);
    useEffect(() => {
      testState.dashboardListeners.add(bump);
      return () => void testState.dashboardListeners.delete(bump);
    }, []);
  };
}

vi.mock("@gram/client/react-query/dashboards.js", async () => {
  const useDashboardChanges = await dashboardSubscription();
  return {
    useDashboards: () => {
      useDashboardChanges();
      return {
        isPending: testState.dashboardsPending,
        isError: testState.dashboardsFailed,
        data:
          testState.dashboardsPending || testState.dashboardsFailed
            ? undefined
            : { dashboards: testState.dashboards, builtIn: testState.builtIn },
        refetch: () => {
          testState.dashboardsFailed = false;
          return Promise.resolve();
        },
      };
    },
    invalidateAllDashboards: () => Promise.resolve(),
  };
});
vi.mock("@gram/client/react-query/dashboard.js", async () => {
  const useDashboardChanges = await dashboardSubscription();
  return {
    useDashboard: ({ id }: { id: string }) => {
      useDashboardChanges();
      const found = testState.dashboards.find(
        (dashboard) => dashboard.id === id,
      );
      return {
        isPending: false,
        isError: found === undefined,
        data: found,
        refetch: vi.fn(),
      };
    },
    invalidateAllDashboard: () => Promise.resolve(),
  };
});
vi.mock("@gram/client/react-query/createDashboard.js", () => ({
  useCreateDashboardMutation: mockDashboardWrite("create"),
}));
vi.mock("@gram/client/react-query/updateDashboard.js", () => ({
  useUpdateDashboardMutation: mockDashboardWrite("update"),
}));
vi.mock("@gram/client/react-query/saveDashboardLayout.js", () => ({
  useSaveDashboardLayoutMutation: mockDashboardWrite("saveLayout"),
}));
vi.mock("@gram/client/react-query/saveDashboardFilters.js", () => ({
  useSaveDashboardFiltersMutation: mockDashboardWrite("saveFilters"),
}));
vi.mock("@gram/client/react-query/addDashboardWidget.js", () => ({
  useAddDashboardWidgetMutation: mockDashboardWrite("addWidget"),
}));
vi.mock("@gram/client/react-query/removeDashboardWidget.js", () => ({
  useRemoveDashboardWidgetMutation: mockDashboardWrite("removeWidget"),
}));
vi.mock("@gram/client/react-query/duplicateDashboard.js", () => ({
  useDuplicateDashboardMutation: mockDashboardWrite("duplicate"),
}));
vi.mock("@gram/client/react-query/duplicateBuiltInDashboard.js", () => ({
  useDuplicateBuiltInDashboardMutation: mockDashboardWrite("duplicateBuiltIn"),
}));
vi.mock("@gram/client/react-query/deleteDashboard.js", () => ({
  useDeleteDashboardMutation: mockDashboardWrite("delete"),
}));
// The grid is react-grid-layout's, tested on its own; here a dashboard's
// cards only have to be there, openable and removable.
vi.mock("./DashboardGrid", async () => {
  const { specFromStoredWidget } = await import("./widgetSpec");
  const { Link } = await import("react-router");
  const { encodeSpec } = await import("./exploreUrl");
  const openHref = (spec: ExploreSpec | null | undefined, id?: string) => {
    const params = new URLSearchParams();
    if (spec) params.set("q", encodeSpec(spec));
    if (id) params.set("widget", id);
    return `/explore?${params.toString()}`;
  };
  type Placement = { id: string };
  type Card = { id?: string; name: string } & Parameters<
    typeof specFromStoredWidget
  >[0];
  return {
    DashboardGrid: ({
      cards,
      page,
      canEdit,
      onRemove,
    }: {
      cards: { placement: Placement; widget: Card | undefined }[];
      page?: unknown;
      canEdit: boolean;
      onRemove: (placementId: string) => void;
    }) => (
      <ul
        aria-label="Cards"
        data-editable={canEdit}
        data-page={JSON.stringify(page ?? null)}
      >
        {cards.map(({ placement, widget }) => {
          const name = widget?.name ?? "…";
          return (
            <li key={placement.id}>
              {name}
              {/* A card links to its question, as WidgetView does. */}
              <Link
                to={openHref(
                  widget && specFromStoredWidget(widget),
                  widget?.id,
                )}
              >
                Open {name} in Explore
              </Link>
              <button type="button" onClick={() => onRemove(placement.id)}>
                Remove {name}
              </button>
            </li>
          );
        })}
      </ul>
    ),
  };
});
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
vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: (
      text: string,
      options?: { action?: { label: string; onClick: () => void } },
    ) => {
      testState.toasts.push({ text, ...options });
    },
  },
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
vi.mock("@/routes", async () => {
  const { useNavigate } = await import("react-router");
  return {
    useRoutes: () => {
      const navigate = useNavigate();
      return {
        explore: { href: () => "/explore" },
        dashboards: {
          href: () => "/dashboards",
          goTo: () => void navigate("/dashboards"),
          detail: {
            href: (id: string) => `/dashboards/${id}`,
            goTo: (id: string) => void navigate(`/dashboards/${id}`),
          },
          builtIn: {
            goTo: (slug: string) =>
              void navigate(`/dashboards/builtin/${slug}`),
          },
        },
      };
    },
  };
});
vi.mock("@/components/page-templates", () => ({
  WorkbenchPage: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/page-layout", async () => {
  const { Toolbar } = await import("@/components/ui/Toolbar");
  return { Page: { Eyebrow: () => null, Toolbar } };
});
// The pages' filter bar is the shared one, tested with usePageFilters.
vi.mock("./usePageFilters", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./usePageFilters")>()),
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
      touched: testState.pageTouched,
      apply: (values: unknown) => {
        testState.pageApplied.push(values);
      },
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
        <Routes>
          <Route path="/explore" element={<Explore />} />
          <Route path="/dashboards" element={<DashboardsRoot />}>
            <Route index element={<DashboardsIndex />} />
            <Route path="builtin/:slug" element={<BuiltInDashboardRoute />} />
            <Route path=":dashboardId" element={<DashboardRoute />} />
          </Route>
          <Route path="*" element={null} />
        </Routes>
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
    testState.listFailed = false;
    testState.writes = [];
    testState.projectWrite = false;
    testState.pageContext = {};
    testState.dashboards = [];
    testState.builtIn = [];
    testState.dashboardWrites = [];
    testState.pageTouched = false;
    testState.pageApplied = [];
    testState.toasts = [];
    testState.dashboardsPending = false;
    testState.dashboardsFailed = false;
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
    ).toBe("Count of");
    expect(screen.getByText("all rows")).toBeTruthy();
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

    expect(screen.getByText("This page is not available yet")).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "Dataset" })).toBeNull();
    expect(testState.describeCalls, "it never asks for the catalog").toBe(0);
  });

  it("waits rather than saying no while the flag is still loading", () => {
    testState.flagStatus = "loading";
    renderExplore();

    expect(screen.getByLabelText("Loading the catalog")).toBeTruthy();
    expect(screen.queryByText("This page is not available yet")).toBeNull();
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

    it("offers a stack, and asks for a field to stack by when there is none", () => {
      renderExplore();

      fireEvent.click(screen.getByRole("button", { name: "Stacked bar" }));
      expect(urlSpec()?.chartType).toBe("stacked_bar");
      // The first dataset opens grouped by its default dimension, so the
      // stack has something to stack by until that is cleared.
      expect(screen.queryByText("pick a field to stack by")).toBeNull();
      fireEvent.click(
        screen.getByRole("button", { name: "Stop grouping by user" }),
      );
      expect(screen.getByText("pick a field to stack by")).toBeTruthy();
    });

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
        dashboards: [],
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

    it("says which dashboards a widget is on: in the list, beside Save, and when deleting", async () => {
      const user = userEvent.setup();
      testState.widgets = [
        storedWidget("w-1", "Slow tools", p95ByTool, {
          dashboards: [
            { id: "d-1", name: "Agent activity" },
            { id: "d-2", name: "Costs" },
          ],
        }),
        storedWidget("w-2", "Sessions", p95ByTool),
      ];
      renderExplore();
      showWidgets();
      // Rows are found by name: the list sorts by updated time, and both
      // widgets were saved within the same instant or not.
      const rowOf = (name: string) =>
        screen
          .getAllByRole("row")
          .find((row) => row.textContent?.includes(name));
      expect(rowOf("Slow tools")?.textContent).toContain(
        "On “Agent activity” and “Costs”",
      );
      expect(rowOf("Sessions")?.textContent).toContain("—");

      openWidget("Slow tools");
      expect(screen.queryByText(/Saving changes its card/)).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Bar" }));
      expect(
        screen.getByText(
          "Saving changes its card on “Agent activity” and “Costs”",
        ),
      ).toBeTruthy();

      await user.click(screen.getByRole("button", { name: "Widget actions" }));
      await user.click(screen.getByRole("menuitem", { name: /Delete/ }));
      expect(
        screen.getByText(/Its card goes from “Agent activity” and “Costs” too/),
      ).toBeTruthy();
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

  describe("dashboards", () => {
    const sessionsByUser: ExploreSpec = {
      dataset: "sessions",
      measures: [{ op: "count", field: "*" }],
      filters: [],
      dimensions: ["user"],
      orderBy: "count",
      limit: 0,
      window: "7d",
      chartType: "bar",
    };

    function savedWidget(id: string, name: string) {
      return {
        id,
        name,
        dataset: sessionsByUser.dataset,
        ...widgetFromSpec(sessionsByUser),
        dashboards: [],
        projectId: "project",
        organizationId: "org",
        createdByUserId: "member-1",
        createdAt: new Date(),
        updatedAt: new Date(),
      };
    }

    function dashboard(
      id: string,
      name: string,
      extra: Record<string, unknown> = {},
    ): Record<string, unknown> {
      return {
        id,
        name,
        projectId: "project",
        organizationId: "org",
        createdByUserId: "member-1",
        filters: { values: {} },
        widgets: [],
        createdAt: new Date(),
        updatedAt: new Date(),
        ...extra,
      };
    }

    function param(name: string) {
      return new URLSearchParams(nav.search).get(name);
    }

    // A Speakeasy-built dashboard, as the list endpoint returns it: one
    // number tile, in the shape a saved widget stores.
    const toolCallsCount: ExploreSpec = {
      dataset: "tool_calls",
      measures: [{ op: "count", field: "" }],
      filters: [],
      dimensions: [],
      orderBy: "",
      limit: 0,
      window: "7d",
      chartType: "number",
    };
    function builtInPage() {
      return {
        slug: "mcp-tools",
        name: "MCP & Tools",
        description: "What the agents call",
        cards: [
          {
            name: "Tool calls",
            dataset: "tool_calls",
            ...widgetFromSpec(toolCallsCount),
            x: 0,
            y: 0,
            w: 6,
            h: 2,
          },
        ],
      };
    }

    it("lists the Speakeasy-built dashboards first, opens one read only, and duplicates it into a project dashboard", async () => {
      const user = userEvent.setup();
      testState.builtIn = [builtInPage()];
      testState.dashboards = [dashboard("d-1", "Agent activity")];
      renderExplore("/dashboards");

      expect(screen.getByText("Speakeasy-built")).toBeTruthy();
      const menus = screen.getAllByRole("button", { name: /^Actions for / });
      expect(menus).toHaveLength(2);
      expect(menus[0]).toBe(
        screen.getByRole("button", { name: "Actions for MCP & Tools" }),
      );
      await user.click(menus[0]!);
      expect(screen.getByRole("menuitem", { name: /Duplicate/ })).toBeTruthy();
      expect(screen.queryByRole("menuitem", { name: /Rename/ })).toBeNull();
      expect(screen.queryByRole("menuitem", { name: /Delete/ })).toBeNull();
      await user.keyboard("{Escape}");

      fireEvent.click(screen.getByText("MCP & Tools"));
      expect(nav.pathname).toBe("/dashboards/builtin/mcp-tools");
      expect(screen.getByRole("heading", { name: "MCP & Tools" })).toBeTruthy();
      const cards = screen.getByRole("list", { name: "Cards" });
      expect(cards.getAttribute("data-editable")).toBe("false");
      expect(within(cards).getByText("Tool calls")).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Add widget" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Save filters" })).toBeNull();

      await user.click(screen.getByRole("button", { name: "Duplicate" }));
      expect(testState.dashboardWrites[0]).toEqual({
        kind: "duplicateBuiltIn",
        request: {
          duplicateBuiltInDashboardRequestBody: { slug: "mcp-tools" },
        },
      });
      expect(nav.pathname).toBe("/dashboards/dashboard-1");
      expect(
        screen.getByRole("heading", { name: "MCP & Tools (copy)" }),
      ).toBeTruthy();
      expect(screen.getByRole("button", { name: "Add widget" })).toBeTruthy();
    });

    it("opens a Speakeasy-built card's question in Explore, never as a widget", () => {
      testState.builtIn = [builtInPage()];
      renderExplore("/dashboards/builtin/mcp-tools");

      fireEvent.click(
        screen.getByRole("link", { name: "Open Tool calls in Explore" }),
      );
      expect(nav.pathname).toBe("/explore");
      expect(param("widget")).toBeNull();
      expect(urlSpec()?.dataset).toBe("tool_calls");
    });

    it("lists the project's dashboards, opens one at its own URL, and goes back to the list", () => {
      testState.widgets = [savedWidget("w-1", "Sessions by user")];
      testState.dashboards = [
        dashboard("d-1", "Agent activity", {
          description: "What the agents did",
          widgets: [{ id: "p-1", widgetId: "w-1", x: 0, y: 0, w: 6, h: 3 }],
        }),
      ];
      renderExplore("/dashboards");

      expect(screen.getByText("What the agents did")).toBeTruthy();
      expect(screen.getByText("Test Member")).toBeTruthy();

      fireEvent.click(screen.getByText("Agent activity"));
      expect(nav.pathname).toBe("/dashboards/d-1");
      expect(
        screen.getByRole("heading", { name: "Agent activity" }),
      ).toBeTruthy();
      expect(
        within(screen.getByRole("list", { name: "Cards" })).getByText(
          "Sessions by user",
        ),
      ).toBeTruthy();

      fireEvent.click(screen.getByRole("link", { name: /All dashboards/ }));
      expect(nav.pathname).toBe("/dashboards");
      expect(
        screen.queryByRole("heading", { name: "Agent activity" }),
      ).toBeNull();
      expect(screen.getByText("Agent activity")).toBeTruthy();
    });

    it("makes a dashboard and opens it, empty", async () => {
      const user = userEvent.setup();
      renderExplore("/dashboards");
      expect(screen.getByText("No dashboards yet")).toBeTruthy();

      await user.click(screen.getByRole("button", { name: "New dashboard" }));
      await user.type(
        screen.getByRole("textbox", { name: "Dashboard name" }),
        "Agent activity",
      );
      await user.click(screen.getByRole("button", { name: "Create" }));

      expect(testState.dashboardWrites).toEqual([
        {
          kind: "create",
          request: { createDashboardRequestBody: { name: "Agent activity" } },
        },
      ]);
      expect(nav.pathname).toBe("/dashboards/dashboard-1");
      expect(
        screen.getByRole("heading", { name: "Agent activity" }),
      ).toBeTruthy();
      expect(screen.getByText("Nothing on this dashboard yet")).toBeTruthy();
    });

    it("places a saved widget from the picker and takes a card off again", async () => {
      const user = userEvent.setup();
      testState.widgets = [savedWidget("w-1", "Sessions by user")];
      testState.dashboards = [dashboard("d-1", "Agent activity")];
      renderExplore("/dashboards/d-1");

      await user.click(screen.getByRole("button", { name: "Add widget" }));
      await user.click(
        screen.getByRole("button", { name: /Sessions by user/ }),
      );
      expect(testState.dashboardWrites[0]).toEqual({
        kind: "addWidget",
        request: {
          addDashboardWidgetRequestBody: { id: "d-1", widgetId: "w-1" },
        },
      });
      const cards = screen.getByRole("list", { name: "Cards" });
      expect(within(cards).getByText("Sessions by user")).toBeTruthy();

      fireEvent.click(
        screen.getByRole("button", { name: "Remove Sessions by user" }),
      );
      expect(testState.dashboardWrites[1]).toEqual({
        kind: "removeWidget",
        request: {
          removeDashboardWidgetRequestBody: {
            id: "d-1",
            placementId: "placement-1",
          },
        },
      });
    });

    it("opens a card's question in Explore, as the saved widget", () => {
      testState.widgets = [savedWidget("w-1", "Sessions by user")];
      testState.dashboards = [
        dashboard("d-1", "Agent activity", {
          widgets: [{ id: "p-1", widgetId: "w-1", x: 0, y: 0, w: 6, h: 3 }],
        }),
      ];
      renderExplore("/dashboards/d-1");

      fireEvent.click(
        screen.getByRole("link", { name: "Open Sessions by user in Explore" }),
      );
      expect(nav.pathname).toBe("/explore");
      expect(param("widget")).toBe("w-1");
      expect(urlSpec()).toMatchObject({
        dataset: "sessions",
        chartType: "bar",
      });
      expect(screen.getByRole("button", { name: "Run query" })).toBeTruthy();
    });

    it("lets someone else's dashboard be read and duplicated, not changed, without project write", async () => {
      const user = userEvent.setup();
      testState.dashboards = [
        dashboard("d-1", "Agent activity", { createdByUserId: "other" }),
      ];
      renderExplore("/dashboards/d-1");

      expect(screen.queryByRole("button", { name: "Add widget" })).toBeNull();
      await user.click(
        screen.getByRole("button", { name: "Actions for Agent activity" }),
      );
      expect(screen.queryByRole("menuitem", { name: /Rename/ })).toBeNull();
      expect(screen.queryByRole("menuitem", { name: /Delete/ })).toBeNull();
      await user.click(screen.getByRole("menuitem", { name: /Duplicate/ }));

      expect(testState.dashboardWrites[0]?.kind).toBe("duplicate");
      expect(nav.pathname).toBe("/dashboards/dashboard-1");
      expect(
        screen.getByRole("heading", { name: "Agent activity (copy)" }),
      ).toBeTruthy();
      expect(screen.getByRole("button", { name: "Add widget" })).toBeTruthy();
    });

    it("opens on its saved filters, offering the fields its cards can be filtered by, and folds the bar into every card", () => {
      testState.widgets = [savedWidget("w-1", "Sessions by user")];
      testState.dashboards = [
        dashboard("d-1", "Agent activity", {
          filters: { range: { preset: "30d" }, values: { user: ["alice"] } },
          widgets: [{ id: "p-1", widgetId: "w-1", x: 0, y: 0, w: 6, h: 3 }],
        }),
      ];
      testState.pageContext = {
        window: { preset: "30d", customRange: null, customLabel: null },
        filters: { user: ["alice"] },
      };
      renderExplore("/dashboards/d-1");

      // The sessions dataset filters by user and surface, not model.
      expect(testState.pageFilterConfig).toMatchObject({
        fields: [
          { field: "user", label: "User" },
          { field: "surface", label: "Agent" },
        ],
        defaultPreset: "30d",
        optionsDatasets: ["sessions"],
      });
      expect(testState.pageApplied).toEqual([
        {
          window: { preset: "30d", customRange: null, customLabel: null },
          filters: { user: ["alice"], surface: [] },
        },
      ]);
      const cards = screen.getByRole("list", { name: "Cards" });
      expect(JSON.parse(cards.getAttribute("data-page") ?? "null")).toEqual({
        window: { preset: "30d", customRange: null, customLabel: null },
        filters: { user: ["alice"] },
      });
      // The bar matches what is saved, so there is nothing to save or reset.
      expect(screen.queryByRole("button", { name: "Save filters" })).toBeNull();
      expect(
        screen.queryByRole("button", { name: "Reset filters" }),
      ).toBeNull();
    });

    it("offers nothing to save until the bar has opened on the saved filters", () => {
      testState.listPending = true;
      testState.dashboards = [
        dashboard("d-1", "Agent activity", {
          filters: { range: { preset: "30d" }, values: {} },
          widgets: [{ id: "p-1", widgetId: "w-1", x: 0, y: 0, w: 6, h: 3 }],
        }),
      ];
      // The bar still shows its defaults, which differ from what is saved.
      testState.pageContext = {
        window: { preset: "7d", customRange: null, customLabel: null },
        filters: {},
      };
      renderExplore("/dashboards/d-1");

      expect(testState.pageApplied).toEqual([]);
      expect(screen.queryByRole("button", { name: "Save filters" })).toBeNull();
      expect(
        screen.queryByRole("button", { name: "Reset filters" }),
      ).toBeNull();
    });

    it("leaves a link that says what to show alone", () => {
      testState.widgets = [savedWidget("w-1", "Sessions by user")];
      testState.dashboards = [
        dashboard("d-1", "Agent activity", {
          filters: { range: { preset: "30d" }, values: {} },
          widgets: [{ id: "p-1", widgetId: "w-1", x: 0, y: 0, w: 6, h: 3 }],
        }),
      ];
      testState.pageTouched = true;
      renderExplore("/dashboards/d-1?range=1d");

      expect(testState.pageApplied).toEqual([]);
    });

    it("saves the bar as what the dashboard opens on, for its editor, and resets to it for anyone", async () => {
      const user = userEvent.setup();
      testState.widgets = [savedWidget("w-1", "Sessions by user")];
      testState.dashboards = [
        dashboard("d-1", "Agent activity", {
          widgets: [{ id: "p-1", widgetId: "w-1", x: 0, y: 0, w: 6, h: 3 }],
        }),
      ];
      // The viewer has picked a range and a user the dashboard does not save.
      testState.pageTouched = true;
      testState.pageContext = {
        window: { preset: "30d", customRange: null, customLabel: null },
        filters: { user: ["alice"] },
      };
      renderExplore("/dashboards/d-1?range=30d&user=alice");

      await user.click(screen.getByRole("button", { name: "Reset filters" }));
      expect(testState.pageApplied).toEqual([
        {
          window: { preset: "7d", customRange: null, customLabel: null },
          filters: { user: [], surface: [] },
        },
      ]);

      await user.click(screen.getByRole("button", { name: "Save filters" }));
      expect(testState.dashboardWrites).toEqual([
        {
          kind: "saveFilters",
          request: {
            saveDashboardFiltersRequestBody: {
              id: "d-1",
              filters: {
                range: { preset: "30d" },
                values: { user: ["alice"] },
              },
            },
          },
        },
      ]);
    });

    it("offers a reader no Save filters, only Reset", () => {
      testState.widgets = [savedWidget("w-1", "Sessions by user")];
      testState.dashboards = [
        dashboard("d-1", "Agent activity", {
          createdByUserId: "other",
          widgets: [{ id: "p-1", widgetId: "w-1", x: 0, y: 0, w: 6, h: 3 }],
        }),
      ];
      testState.pageTouched = true;
      testState.pageContext = {
        window: { preset: "30d", customRange: null, customLabel: null },
        filters: {},
      };
      renderExplore("/dashboards/d-1?range=30d");

      expect(screen.queryByRole("button", { name: "Save filters" })).toBeNull();
      expect(
        screen.getByRole("button", { name: "Reset filters" }),
      ).toBeTruthy();
    });

    it("says when the widgets behind the cards did not load, and tries again", () => {
      testState.listFailed = true;
      testState.dashboards = [
        dashboard("d-1", "Agent activity", {
          widgets: [{ id: "p-1", widgetId: "w-1", x: 0, y: 0, w: 6, h: 3 }],
        }),
      ];
      renderExplore("/dashboards/d-1");

      expect(screen.getByText("The widgets did not load")).toBeTruthy();
      expect(screen.queryByRole("list", { name: "Cards" })).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Try again" }));
      expect(testState.listFailed).toBe(false);
    });

    it("places a widget on a dashboard from its row, and offers to open it", async () => {
      const user = userEvent.setup();
      testState.widgets = [savedWidget("w-1", "Sessions by user")];
      testState.dashboards = [
        dashboard("d-1", "Agent activity"),
        dashboard("d-2", "Someone else's", { createdByUserId: "other" }),
      ];
      renderExplore("/explore?tab=widgets");

      await user.click(
        screen.getByRole("button", { name: "Actions for Sessions by user" }),
      );
      await user.click(
        screen.getByRole("menuitem", { name: /Add to dashboard/ }),
      );
      const dialog = screen.getByRole("dialog");
      expect(
        within(dialog).getByText("Add “Sessions by user” to a dashboard"),
      ).toBeTruthy();
      // Only dashboards the viewer may change are offered.
      expect(within(dialog).queryByText("Someone else's")).toBeNull();
      await user.click(
        within(dialog).getByRole("button", { name: /Agent activity/ }),
      );

      expect(testState.dashboardWrites).toEqual([
        {
          kind: "addWidget",
          request: {
            addDashboardWidgetRequestBody: { id: "d-1", widgetId: "w-1" },
          },
        },
      ]);
      expect(screen.queryByRole("dialog")).toBeNull();
      const [toast] = testState.toasts;
      expect(toast?.text).toBe("Added to “Agent activity”");
      act(() => toast?.action?.onClick());
      expect(nav.pathname).toBe("/dashboards/d-1");
    });

    it("says while the dashboards load, and when they did not, rather than offering none", async () => {
      const user = userEvent.setup();
      testState.widgets = [savedWidget("w-1", "Sessions by user")];
      const openPicker = async () => {
        await user.click(
          screen.getByRole("button", { name: "Actions for Sessions by user" }),
        );
        await user.click(
          screen.getByRole("menuitem", { name: /Add to dashboard/ }),
        );
        return screen.getByRole("dialog");
      };

      testState.dashboardsPending = true;
      const { unmount } = renderExplore("/explore?tab=widgets");
      let dialog = await openPicker();
      expect(dialog.querySelector("[aria-busy='true']")).toBeTruthy();
      expect(
        within(dialog).queryByText("The dashboards could not be fetched."),
      ).toBeNull();
      expect(within(dialog).queryByText(/yours to change yet/)).toBeNull();
      unmount();

      testState.dashboardsPending = false;
      testState.dashboardsFailed = true;
      renderExplore("/explore?tab=widgets");
      dialog = await openPicker();
      expect(
        within(dialog).getByText("The dashboards could not be fetched."),
      ).toBeTruthy();
      expect(dialog.querySelector("[aria-busy='true']")).toBeNull();
      expect(within(dialog).queryByText(/yours to change yet/)).toBeNull();
      await user.click(
        within(dialog).getByRole("button", { name: "Try again" }),
      );
      expect(testState.dashboardsFailed).toBe(false);
    });

    it("makes a new dashboard for a widget when none is theirs to change", async () => {
      const user = userEvent.setup();
      testState.widgets = [savedWidget("w-1", "Sessions by user")];
      renderExplore("/explore?tab=widgets");

      await user.click(
        screen.getByRole("button", { name: "Actions for Sessions by user" }),
      );
      await user.click(
        screen.getByRole("menuitem", { name: /Add to dashboard/ }),
      );
      await user.click(screen.getByRole("button", { name: "New dashboard" }));
      await user.type(
        screen.getByRole("textbox", { name: "Dashboard name" }),
        "Agent activity",
      );
      await user.click(screen.getByRole("button", { name: "Create and add" }));

      expect(testState.dashboardWrites.map((write) => write.kind)).toEqual([
        "create",
        "addWidget",
      ]);
      expect(testState.dashboardWrites[1]?.request).toEqual({
        addDashboardWidgetRequestBody: {
          id: "dashboard-1",
          widgetId: "w-1",
        },
      });
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(testState.toasts[0]?.text).toBe("Added to “Agent activity”");
    });

    it("saves the builder as a widget straight onto a dashboard, from Advanced", async () => {
      const user = userEvent.setup();
      testState.dashboards = [dashboard("d-1", "Agent activity")];
      renderExplore();
      fireEvent.click(screen.getByRole("button", { name: "Save widget" }));
      fireEvent.change(screen.getByRole("textbox", { name: "Widget name" }), {
        target: { value: "Sessions by user" },
      });
      await user.click(screen.getByRole("button", { name: "Advanced" }));
      const pick = screen.getByRole("combobox", { name: "Dashboard" });
      expect(pick.textContent).toBe("No dashboard");
      fireEvent.keyDown(pick, { key: "ArrowDown" });
      fireEvent.keyDown(screen.getByRole("option", { name: "No dashboard" }), {
        key: "ArrowDown",
      });
      fireEvent.keyDown(
        screen.getByRole("option", { name: "Agent activity" }),
        { key: "Enter" },
      );
      expect(pick.textContent).toBe("Agent activity");
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      expect(testState.writes[0]?.kind).toBe("create");
      expect(testState.dashboardWrites).toEqual([
        {
          kind: "addWidget",
          request: {
            addDashboardWidgetRequestBody: {
              id: "d-1",
              widgetId: "created-1",
            },
          },
        },
      ]);
      expect(param("widget")).toBe("created-1");
      expect(testState.toasts[0]?.text).toBe("Added to “Agent activity”");
    });

    it("deletes a dashboard from its page and returns to the list", async () => {
      const user = userEvent.setup();
      testState.dashboards = [dashboard("d-1", "Agent activity")];
      renderExplore("/dashboards/d-1");

      await user.click(
        screen.getByRole("button", { name: "Actions for Agent activity" }),
      );
      await user.click(screen.getByRole("menuitem", { name: /Delete/ }));
      await user.click(screen.getByRole("button", { name: "Delete" }));

      expect(testState.dashboardWrites).toEqual([
        { kind: "delete", request: { id: "d-1" } },
      ]);
      expect(nav.pathname).toBe("/dashboards");
      expect(screen.getByText("No dashboards yet")).toBeTruthy();
    });
  });
});
