import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { UserSessionTrend } from "@gram/client/models/components/usersessiontrend.js";
import { GatewayConnectionTrend } from "./GatewayConnectionTrend";
import type { HealthWindow } from "./connectionHealth";
import { trendChartData } from "./connectionTrend";

type Query = {
  data?: UserSessionTrend;
  error?: unknown;
  isError?: boolean;
  refetch?: () => void;
};

const state = vi.hoisted(() => ({
  query: {} as Query,
  calls: [] as unknown[][],
  charts: [] as {
    datasets: { label: string; stack?: string; data: number[] }[];
    tooltipLabels: string[];
  }[],
  scopes: ["project:read", "mcp:read"] as string[],
  rbacLoading: false,
}));
vi.mock("@gram/client/react-query/userSessionTrend.js", () => ({
  useUserSessionTrend: (...args: unknown[]) => {
    state.calls.push(args);
    return state.query;
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "default" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: state.rbacLoading,
    hasScope: (scope: string, resource: string, project?: string) =>
      state.scopes.includes(scope) &&
      (scope === "project:read"
        ? resource === "project-1"
        : resource === "gateway-1" && project === "project-1"),
  }),
}));
vi.mock("@/components/chart/StackedTimeBarChart", () => ({
  StackedTimeBarChart: (props: (typeof state.charts)[number]) => {
    state.charts.push(props);
    return <div>chart</div>;
  },
}));

const from = new Date("2026-01-01T00:00:00.000Z");
const to = new Date("2026-01-01T03:00:00.000Z");
const hour = (h: number) => new Date(from.getTime() + h * 3_600_000);

function trendOf(overrides: Partial<UserSessionTrend> = {}): UserSessionTrend {
  return {
    from,
    to,
    bucket: "hour",
    buckets: [
      { start: hour(0), end: hour(1), created: 2, revoked: 0, active: 3 },
      { start: hour(1), end: hour(2), created: 0, revoked: 0, active: 3 },
      { start: hour(2), end: hour(3), created: 0, revoked: 1, active: 2 },
    ],
    activeAtStart: 1,
    created: 2,
    revoked: 1,
    activeAtEnd: 2,
    excludedLegacyRows: 0,
    unattributedRows: 0,
    ...overrides,
  };
}

beforeEach(() => {
  state.query = { data: trendOf() };
  state.calls = [];
  state.charts = [];
  state.scopes = ["project:read", "mcp:read"];
  state.rbacLoading = false;
});
afterEach(cleanup);

function renderTrend(span: HealthWindow = "24h") {
  return render(
    <TooltipProvider>
      <GatewayConnectionTrend
        gatewayId="gateway-1"
        span={span}
        from={from}
        to={to}
        phrase="in the last 24 hours"
      />
    </TooltipProvider>,
  );
}

const tile = (title: string) =>
  screen
    .getAllByText(title)
    .map((node) => node.closest("[data-tone]"))
    .find(Boolean)?.textContent ?? "";

it("reads the gateway's trend for the shared window, hourly up to a week and daily beyond", () => {
  for (const [span, bucket] of [
    ["24h", "hour"],
    ["7d", "hour"],
    ["30d", "day"],
  ] as const) {
    renderTrend(span);
    expect(state.calls.at(-1)![0]).toEqual({
      gramProject: "default",
      metaMcpServerId: "gateway-1",
      from,
      to,
      bucket,
    });
    expect(state.calls.at(-1)![2]).toMatchObject({ enabled: true });
    cleanup();
  }
});

it("reads nothing and shows nothing without project and gateway read", () => {
  for (const scopes of [["project:read"], ["mcp:read"]]) {
    state.scopes = scopes;
    const { container } = renderTrend();
    expect(container.innerHTML).toBe("");
    expect(state.calls.at(-1)![2]).toMatchObject({ enabled: false });
    cleanup();
  }
});

it("shows no cached history, error or retry while grants are unresolved", () => {
  state.scopes = ["project:read", "mcp:read"];
  state.rbacLoading = true;
  for (const query of [
    { data: trendOf() },
    {
      data: trendOf(),
      error: new Error("boom"),
      isError: true,
      refetch: vi.fn<() => void>(),
    },
  ]) {
    state.query = query;
    state.charts = [];
    renderTrend();
    expect(state.calls.at(-1)![2]).toMatchObject({ enabled: false });
    expect(screen.getByText("Connections over time")).toBeTruthy();
    expect(screen.queryByText("Active at start")).toBeNull();
    expect(screen.queryByText("Created")).toBeNull();
    expect(state.charts).toHaveLength(0);
    expect(screen.queryByText(/Couldn't load/)).toBeNull();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    cleanup();
  }
});

it("labels both event series under the collapsed chart", () => {
  renderTrend();
  const legend = screen.getByRole("list", { name: "Series" });
  expect(legend.textContent).toContain("Created");
  expect(legend.textContent).toContain("Revoked");
});

it("charts a window whose only connection expires in its first interval", () => {
  state.query = {
    data: trendOf({
      buckets: trendOf().buckets.map((b) => ({
        ...b,
        created: 0,
        revoked: 0,
        active: 0,
      })),
      activeAtStart: 1,
      created: 0,
      revoked: 0,
      activeAtEnd: 0,
    }),
  };
  renderTrend();
  expect(screen.queryByText(/No connections to this gateway/)).toBeNull();
  expect(state.charts).toHaveLength(2);
  expect(tile("Active at start")).toContain("1");
});

it("summarizes the window and keeps events apart from the active stock", () => {
  renderTrend();
  expect(tile("Active at start")).toContain("1");
  expect(tile("Created")).toContain("2");
  expect(tile("Revoked")).toContain("1");
  expect(tile("Active at end")).toContain("2");
  const [events, active] = state.charts;
  expect(events!.datasets.map((d) => [d.label, d.stack, d.data])).toEqual([
    ["Created", "created", [2, 0, 0]],
    ["Revoked", "revoked", [0, 0, 1]],
  ]);
  expect(active!.datasets.map((d) => [d.label, d.stack, d.data])).toEqual([
    ["Active", undefined, [3, 3, 2]],
  ]);
  expect(active!.tooltipLabels[2]).toMatch(/^Just before /);
  expect(
    screen.getByText(
      /does not mean it worked, could reach its tools, or was used/,
    ),
  ).toBeTruthy();
  expect(screen.queryByText(/may be incomplete/)).toBeNull();
});

it("says older history is incomplete without counting it as connections", () => {
  state.query = {
    data: trendOf({ excludedLegacyRows: 4, unattributedRows: 0 }),
  };
  renderTrend();
  const note = screen.getByText(/may be incomplete/).textContent ?? "";
  expect(note).not.toMatch(/\d/);
  cleanup();
  state.query = {
    data: trendOf({ excludedLegacyRows: 0, unattributedRows: 2 }),
  };
  renderTrend();
  expect(screen.getByText(/may be incomplete/)).toBeTruthy();
});

it("shows an empty window as empty, with its summary", () => {
  state.query = {
    data: trendOf({
      buckets: trendOf().buckets.map((b) => ({
        ...b,
        created: 0,
        revoked: 0,
        active: 0,
      })),
      activeAtStart: 0,
      created: 0,
      revoked: 0,
      activeAtEnd: 0,
    }),
  };
  renderTrend();
  expect(
    screen.getByText("No connections to this gateway in the last 24 hours."),
  ).toBeTruthy();
  expect(state.charts).toHaveLength(0);
  expect(tile("Active at end")).toContain("0");
  cleanup();
  // Records left out mean a quiet window is not proof of no connections.
  state.query = {
    data: trendOf({
      buckets: trendOf().buckets.map((b) => ({
        ...b,
        created: 0,
        revoked: 0,
        active: 0,
      })),
      activeAtStart: 0,
      created: 0,
      revoked: 0,
      activeAtEnd: 0,
      excludedLegacyRows: 2,
    }),
  };
  renderTrend();
  expect(
    screen.getByText(
      "No recorded connection history to chart in the last 24 hours.",
    ),
  ).toBeTruthy();
  expect(screen.getByText(/may be incomplete/)).toBeTruthy();
});

it("never shows an answer for another window or one a failed refetch kept", () => {
  state.query = { data: trendOf({ to: hour(4) }) };
  renderTrend();
  expect(screen.queryByText("Active at end")).toBeNull();
  cleanup();
  const refetch = vi.fn<() => void>();
  state.query = {
    data: trendOf(),
    error: new Error("boom"),
    isError: true,
    refetch,
  };
  renderTrend();
  expect(screen.queryByText("Active at end")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(refetch).toHaveBeenCalled();
  cleanup();
  state.query = {
    error: Object.assign(new Error("hidden"), { statusCode: 404 }),
    isError: true,
  };
  renderTrend();
  expect(
    screen.getByText(
      "This gateway's connection history is not available to you.",
    ),
  ).toBeTruthy();
});

// Buckets are UTC; labels must not move with the viewer's zone.
function inZone<T>(zone: string, run: () => T): T {
  const previous = process.env.TZ;
  process.env.TZ = zone;
  try {
    return run();
  } finally {
    if (previous === undefined) delete process.env.TZ;
    else process.env.TZ = previous;
  }
}

it("labels UTC buckets in UTC west of UTC and in half-hour zones", () => {
  // Expected text from the viewer's locale, but always in UTC; the local day
  // west of UTC is a different one, which proves the zone is pinned.
  const utcDay = (date: Date) =>
    new Intl.DateTimeFormat(undefined, {
      timeZone: "UTC",
      month: "short",
      day: "numeric",
    }).format(date);
  const utcTime = (date: Date) =>
    new Intl.DateTimeFormat(undefined, {
      timeZone: "UTC",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
    }).format(date);
  const midnight = new Date("2026-09-26T00:00:00.000Z");
  const nextMidnight = new Date("2026-09-27T00:00:00.000Z");
  const daily = trendOf({
    bucket: "day",
    from: midnight,
    to: new Date("2026-09-28T00:00:00.000Z"),
    buckets: [
      { start: midnight, end: nextMidnight, created: 1, revoked: 0, active: 1 },
      {
        start: nextMidnight,
        end: new Date("2026-09-28T00:00:00.000Z"),
        created: 0,
        revoked: 0,
        active: 1,
      },
    ],
  });
  const losAngeles = inZone("America/Los_Angeles", () => ({
    charts: trendChartData(daily, daily.from, daily.to, "#123456"),
    localDay: new Intl.DateTimeFormat(undefined, {
      month: "short",
      day: "numeric",
    }).format(midnight),
  }));
  expect(losAngeles.charts.events.labels).toEqual([
    utcDay(midnight),
    utcDay(nextMidnight),
  ]);
  expect(losAngeles.charts.events.labels[0]).not.toBe(losAngeles.localDay);
  expect(losAngeles.charts.events.tooltipLabels[0]).toBe(
    `${utcDay(midnight)}, ${utcTime(midnight)} – ${utcDay(nextMidnight)}, ${utcTime(nextMidnight)} UTC`,
  );
  expect(losAngeles.charts.active.tooltipLabels[0]).toBe(
    `Just before ${utcDay(nextMidnight)}, ${utcTime(nextMidnight)} UTC`,
  );

  const fourteen = new Date("2026-09-26T14:00:00.000Z");
  const fifteen = new Date("2026-09-26T15:00:00.000Z");
  const hourly = trendOf({
    from: fourteen,
    to: new Date("2026-09-26T16:00:00.000Z"),
    buckets: [
      { start: fourteen, end: fifteen, created: 1, revoked: 0, active: 1 },
      {
        start: fifteen,
        end: new Date("2026-09-26T16:00:00.000Z"),
        created: 0,
        revoked: 0,
        active: 1,
      },
    ],
  });
  const kolkata = inZone("Asia/Kolkata", () =>
    trendChartData(hourly, hourly.from, hourly.to, "#123456"),
  );
  expect(kolkata.events.labels).toEqual(["14:00", "15:00"]);
  expect(kolkata.events.tooltipLabels[0]).toBe(
    `${utcDay(fourteen)}, 14:00 – ${utcDay(fifteen)}, 15:00 UTC`,
  );
});

it("charts created and revoked in separate stacks so they are never summed", () => {
  const charts = trendChartData(trendOf(), from, to, "#123456");
  expect(new Set(charts.events.datasets.map((d) => d.stack)).size).toBe(2);
  expect(charts.active.datasets[0]!.backgroundColor).toBe("#123456");
  // The interval is the bucket width even when the first bucket is clipped.
  const start = new Date(from.getTime() + 20 * 60_000);
  const clipped = trendOf({
    from: start,
    buckets: [
      { start, end: hour(1), created: 0, revoked: 0, active: 0 },
      { start: hour(1), end: hour(2), created: 0, revoked: 0, active: 0 },
    ],
  });
  expect(trendChartData(clipped, start, to, "#123456").events.bucketMs).toBe(
    3_600_000,
  );
  expect(
    trendChartData(trendOf({ bucket: "day" }), from, to, "#123456").active
      .bucketMs,
  ).toBe(86_400_000);
});
