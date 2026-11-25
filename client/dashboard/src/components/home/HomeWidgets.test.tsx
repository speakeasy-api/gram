import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { formatSince } from "@/pages/mcp-sessions/connection-health/format";

const state = vi.hoisted(() => ({
  flag: "enabled" as "enabled" | "disabled" | "loading",
  scopes: ["log:read"] as string[],
  rbacLoading: false,
  logsEnabled: true,
  projectId: "project-1",
  connections: {
    ok: true,
    loggingEnabled: true,
    viewerScope: "all",
    problems: [] as Record<string, unknown>[],
    total: undefined as number | undefined,
  },
  connectionCalls: [] as unknown[],
  failingTools: [] as Record<string, unknown>[],
  weekFails: false,
  signals: [] as Record<string, unknown>[],
  signalsFail: false,
}));

const unavailable = () =>
  new GramError("unavailable", {
    response: new Response("", { status: 503 }),
    request: new Request("http://localhost/rpc"),
    body: "",
  });

vi.mock(
  "@gram/client/funcs/mcpConnectionHealthListProjectSuspectedProblems.js",
  () => ({
    mcpConnectionHealthListProjectSuspectedProblems: (
      _client: unknown,
      request: unknown,
    ) => {
      state.connectionCalls.push(request);
      const c = state.connections;
      return Promise.resolve(
        c.ok
          ? {
              ok: true,
              value: {
                loggingEnabled: c.loggingEnabled,
                connectionViewerScope: c.viewerScope,
                problems: c.problems,
                problemsTotal: c.total ?? c.problems.length,
              },
            }
          : { ok: false, error: unavailable() },
      );
    },
  }),
);
vi.mock("@gram/client/funcs/telemetryGetObservabilityOverview", () => ({
  telemetryGetObservabilityOverview: () =>
    Promise.resolve(
      state.weekFails
        ? { ok: false, error: unavailable() }
        : {
            ok: true,
            value: {
              topToolsByFailureRate: state.failingTools,
              timeSeries: [],
              summary: { totalCost: 0 },
              comparison: { totalCost: 0 },
            },
          },
    ),
}));
vi.mock("@gram/client/funcs/telemetryGetToolUsageUsers", () => ({
  telemetryGetToolUsageUsers: () =>
    Promise.resolve({ ok: true, value: { users: [] } }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data: { logsEnabled: state.logsEnabled },
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/riskSignals.js", () => ({
  useRiskSignals: (_req: unknown, _sec: unknown, opts: { enabled: boolean }) =>
    opts.enabled
      ? {
          data: state.signalsFail ? undefined : { signals: state.signals },
          isPending: false,
          isError: state.signalsFail,
        }
      : { data: undefined, isPending: true, isError: false },
}));
vi.mock("@gram/client/react-query/accessRequests.js", () => ({
  useAccessRequests: () => ({
    data: undefined,
    isPending: true,
    isError: false,
  }),
}));
vi.mock("@/components/project/projectOverviewQuery", () => ({
  buildProjectOverviewQuery: (_client: unknown, args: unknown) => ({
    queryKey: ["overview", args],
    queryFn: () => Promise.resolve({ summary: { topServers: [] } }),
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: (name: string) => ({
    status: name === "mcp-connection-health" ? state.flag : "disabled",
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    // Project scopes count only for the project asked about.
    hasScope: (scope: string, resourceId?: string) =>
      !state.rbacLoading &&
      state.scopes.includes(scope) &&
      (scope === "org:admin" || resourceId === state.projectId),
    isLoading: state.rbacLoading,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
  useProject: () => ({ id: state.projectId, slug: "project" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));
vi.mock("@/components/insights-context", () => ({
  useInsightsState: () => ({ sendPrompt: () => undefined }),
}));
vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: () => ({ data: [], isError: false }),
}));
vi.mock("@/components/tool-usage-identity", () => ({
  ToolUsageIdentity: () => null,
}));
vi.mock("@/components/chart/stat-tile", () => ({ StatTile: () => null }));
vi.mock("@/pages/costs/Sparkline", () => ({ Sparkline: () => null }));
vi.mock("@/components/ui/Skeleton", () => ({
  Skeleton: () => <div data-testid="skeleton" />,
}));
vi.mock("@/components/ui/Card", () => ({
  Card: {
    Dashboard: ({
      title,
      tooltip,
      action,
      children,
    }: {
      title: string;
      tooltip?: string;
      action?: ReactNode;
      children: ReactNode;
    }) => (
      <section aria-label={title} data-tooltip={tooltip}>
        <header>{action}</header>
        {children}
      </section>
    ),
  },
}));

import { HomeWidgets } from "./HomeWidgets";
import { useConnectionProblemsAccess } from "./useConnectionProblems";

const NOW = new Date(2026, 10, 20, 12, 0);
const today = (h: number, m: number) => new Date(2026, 10, 20, h, m);

function problem(
  id: string,
  extra: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    id,
    headline: `Problem ${id}`,
    fact: `Fact ${id}`,
    since: today(9, 5),
    lastFailedAt: today(11, 0),
    suspectedProblemPart: "service",
    suspectedProblemRule: "same_server",
    peopleCount: 2,
    scope: { kind: "server", id: "srv-1", name: "Wiki" },
    filters: { from: today(9, 0) },
    ...extra,
  };
}

function renderWidgets(): { rerender: () => void } {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const tree = () => (
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <HomeWidgets classNames={["", "", "", "", ""]} />
      </MemoryRouter>
    </QueryClientProvider>
  );
  const view = render(tree());
  return { rerender: () => view.rerender(tree()) };
}

const failingTool = {
  gramUrn: "tools:http:x:search",
  failureCount: 3,
  failureRate: 0.5,
  callCount: 6,
};
const criticalSignal = {
  ruleId: "rule-1",
  description: "Agent reads secrets",
  findings: 4,
  severity: "critical",
};

const card = () => screen.getByRole("region", { name: "Needs attention" });
const rows = () => within(card()).queryAllByRole("link");
const PARTIAL = "Some sources failed to load, so this list may be incomplete.";
const EMPTY = "Nothing needs attention this week.";
const LOAD_ERROR = "Couldn't load this data. Try reloading the page.";

async function settled(): Promise<void> {
  await waitFor(() =>
    expect(within(card()).queryAllByTestId("skeleton")).toHaveLength(0),
  );
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"], now: NOW });
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  state.flag = "enabled";
  state.scopes = ["log:read"];
  state.rbacLoading = false;
  state.logsEnabled = true;
  state.projectId = "project-1";
  state.connections = {
    ok: true,
    loggingEnabled: true,
    viewerScope: "all",
    problems: [],
    total: undefined,
  };
  state.connectionCalls = [];
  state.failingTools = [];
  state.weekFails = false;
  state.signals = [];
  state.signalsFail = false;
});

describe("Needs attention connection rows", () => {
  it("waits on the flag and does not read problems while it loads", async () => {
    state.flag = "loading";
    state.connections.problems = [problem("a")];
    renderWidgets();
    await act(async () => {});
    expect(within(card()).getAllByTestId("skeleton").length).toBeGreaterThan(0);
    expect(state.connectionCalls).toHaveLength(0);
  });

  it("waits on the viewer's permissions and does not read problems while they load", async () => {
    state.rbacLoading = true;
    state.connections.problems = [problem("a")];
    renderWidgets();
    await act(async () => {});
    expect(within(card()).getAllByTestId("skeleton").length).toBeGreaterThan(0);
    expect(state.connectionCalls).toHaveLength(0);
  });

  it("reads nothing and shows no rows with the flag off", async () => {
    state.flag = "disabled";
    state.connections.problems = [problem("a")];
    renderWidgets();
    await settled();
    expect(state.connectionCalls).toHaveLength(0);
    expect(within(card()).getByText(EMPTY)).toBeTruthy();
  });

  it("reads nothing and shows no rows without log:read", async () => {
    state.scopes = [];
    state.connections.problems = [problem("a")];
    renderWidgets();
    await settled();
    expect(state.connectionCalls).toHaveLength(0);
    expect(rows()).toHaveLength(0);
    expect(within(card()).getByText(EMPTY)).toBeTruthy();
  });

  it("reads nothing with log:read on another project only", async () => {
    state.projectId = "project-2";
    state.scopes = [];
    renderWidgets();
    await settled();
    expect(state.connectionCalls).toHaveLength(0);
  });

  it("reads nothing with logs off for the organization", async () => {
    state.logsEnabled = false;
    state.connections.problems = [problem("a")];
    renderWidgets();
    await settled();
    expect(state.connectionCalls).toHaveLength(0);
    expect(rows()).toHaveLength(0);
  });

  it("reads the problems again for another project", async () => {
    state.connections.problems = [problem("a")];
    const view = renderWidgets();
    await settled();
    expect(rows().map((l) => l.textContent)).toEqual([
      expect.stringContaining("Problem a"),
    ]);
    state.projectId = "project-2";
    state.scopes = ["log:read"];
    state.connections.problems = [problem("b")];
    view.rerender();
    await waitFor(() =>
      expect(rows().map((l) => l.textContent)).toEqual([
        expect.stringContaining("Problem b"),
      ]),
    );
    expect(state.connectionCalls).toHaveLength(2);
  });

  it("names connection problems in the tooltip only for viewers who get them", async () => {
    renderWidgets();
    await settled();
    expect(card().getAttribute("data-tooltip")).toMatch(
      /^People who can't connect/,
    );
    cleanup();
    state.flag = "disabled";
    renderWidgets();
    await settled();
    expect(card().getAttribute("data-tooltip")).toBe(
      "High-risk signals, failing tools, and pending access requests from the last 7 days.",
    );
  });

  it("asks for the rows the tall card holds when the flag is on", async () => {
    state.connections.problems = [problem("a")];
    renderWidgets();
    await settled();
    expect(state.connectionCalls).toEqual([
      expect.objectContaining({
        listProjectSuspectedProblemsRequestBody: { limit: 6 },
      }),
    ]);
    expect(rows()).toHaveLength(1);
  });

  it.each([
    ["own scope", { viewerScope: "own" }],
    ["logging off", { loggingEnabled: false }],
  ])("shows no rows and no error for %s", async (_name, patch) => {
    state.connections = {
      ...state.connections,
      problems: [problem("a")],
      ...patch,
    };
    renderWidgets();
    await settled();
    expect(rows()).toHaveLength(0);
    expect(within(card()).queryByText(PARTIAL)).toBeNull();
    expect(within(card()).getByText(EMPTY)).toBeTruthy();
  });

  it("shows no placeholder for zero problems", async () => {
    state.failingTools = [failingTool];
    renderWidgets();
    await settled();
    const list = within(card()).getAllByRole("listitem");
    expect(list).toHaveLength(1);
    expect(list[0]!.textContent).toContain("search is failing");
  });

  it("lists connection rows first, in the API's order, then the card's own", async () => {
    state.scopes = ["log:read", "org:admin"];
    state.signals = [criticalSignal];
    state.connections.problems = [
      problem("b", { server: { id: "srv-b", slug: "wiki" } }),
      problem("a", {
        suspectedProblemRule: "one_person",
        since: new Date(2026, 10, 19, 16, 5),
        server: { id: "srv-a", slug: "desk" },
      }),
    ];
    renderWidgets();
    await settled();
    const links = rows();
    expect(links.map((l) => l.textContent)).toEqual([
      expect.stringContaining("Problem b since"),
      expect.stringContaining("Problem a since"),
      expect.stringContaining("Agent reads secrets"),
    ]);
    const headline = `Problem b since ${formatSince(today(9, 5).toISOString(), NOW)}`;
    expect(within(links[0]!).getByTitle(headline).textContent).toBe(headline);
    expect(within(links[0]!).getByTitle("Fact b")).toBeTruthy();
    expect(links[0]!.hasAttribute("title")).toBe(false);
    expect(links[0]!.textContent).toContain("today");
    expect(links[1]!.textContent).not.toContain("16:05");
  });

  it("links each problem to the page that lists it", async () => {
    state.connections.problems = [
      problem("on-server", {
        server: { id: "srv-1", slug: "wiki" },
        userSessionIssuerId: "iss-1",
      }),
      problem("no-slug", { server: { id: "srv-2" } }),
      problem("reserved-slug", { server: { id: "srv-3", slug: "add" } }),
      problem("sign-in", {
        suspectedProblemPart: "speakeasy",
        userSessionIssuerId: "iss-1",
      }),
      problem("service", {
        userSessionIssuerId: "iss-2",
        filters: { from: today(9, 0), serverIds: ["gw-1"], reasons: ["r1"] },
      }),
      problem("nowhere"),
    ];
    renderWidgets();
    await settled();
    const hrefs = rows().map((l) => l.getAttribute("href") ?? "");
    const base = "/org/projects/project";
    expect(hrefs[0]).toBe(`${base}/mcp/wiki/sessions?problem=on-server`);
    expect(hrefs[1]).toBe(`${base}/mcp/srv-2/sessions?problem=no-slug`);
    expect(hrefs[2]).toBe(`${base}/mcp/srv-3/sessions?problem=reserved-slug`);
    expect(hrefs[3]).toBe(
      `${base}/mcp-sessions/iss-1/overview?problem=sign-in`,
    );
    const logs = new URL(hrefs[4]!, "http://localhost");
    expect(logs.pathname).toBe(`${base}/mcp-sessions/iss-2/connection-logs`);
    expect(logs.searchParams.get("from")).toBe(today(9, 0).toISOString());
    expect(logs.searchParams.get("server")).toBe("gw-1");
    expect(logs.searchParams.get("message")).toBe("r1");
    expect(logs.searchParams.has("problem")).toBe(false);
    expect(hrefs[5]).toBe(`${base}/mcp-sessions`);
  });

  it("offers View all only when there are more problems than shown", async () => {
    state.connections.problems = ["1", "2", "3", "4", "5", "6"].map((id) =>
      problem(id),
    );
    state.connections.total = 9;
    renderWidgets();
    await settled();
    expect(
      rows().filter((l) => l.textContent?.startsWith("Problem")),
    ).toHaveLength(6);
    const viewAll = within(card()).getByRole("link", { name: "View all" });
    expect(viewAll.getAttribute("href")).toBe(
      "/org/projects/project/mcp-sessions",
    );
  });

  it("keeps room for the card's own rows and offers View all for the problems cut", async () => {
    state.scopes = ["log:read", "org:admin"];
    state.signals = [criticalSignal];
    state.connections.problems = ["1", "2", "3", "4", "5", "6"].map((id) =>
      problem(id),
    );
    renderWidgets();
    await settled();
    const texts = rows()
      .filter((l) => l.textContent !== "View all")
      .map((l) => l.textContent);
    expect(texts).toHaveLength(6);
    expect(texts.slice(0, 5)).toEqual(
      ["1", "2", "3", "4", "5"].map((id) =>
        expect.stringContaining(`Problem ${id} since`),
      ),
    );
    expect(texts[5]).toContain("Agent reads secrets");
    expect(within(card()).getByRole("link", { name: "View all" })).toBeTruthy();
  });

  it("has no View all when every problem is shown", async () => {
    state.connections.problems = [problem("1"), problem("2")];
    renderWidgets();
    await settled();
    expect(within(card()).queryByRole("link", { name: "View all" })).toBeNull();
  });

  it("shows the load error, never an empty state, when the read fails and nothing else has rows", async () => {
    state.connections.ok = false;
    renderWidgets();
    await settled();
    expect(within(card()).getByText(LOAD_ERROR)).toBeTruthy();
    expect(within(card()).queryByText(EMPTY)).toBeNull();
  });

  it("keeps the other sources' rows under the partial-load line", async () => {
    state.connections.ok = false;
    state.failingTools = [failingTool];
    renderWidgets();
    await settled();
    expect(within(card()).getByText(PARTIAL)).toBeTruthy();
    expect(within(card()).getByText("search is failing")).toBeTruthy();
  });

  it("shows the load error as before for an admin without connection rows whose risk signals fail", async () => {
    state.flag = "disabled";
    state.scopes = ["org:admin"];
    state.signalsFail = true;
    renderWidgets();
    await settled();
    expect(state.connectionCalls).toHaveLength(0);
    expect(within(card()).getByText(LOAD_ERROR)).toBeTruthy();
    expect(within(card()).queryByText(PARTIAL)).toBeNull();
  });
});

describe("useConnectionProblemsAccess", () => {
  it.each([
    ["on", {}],
    ["off", { logsEnabled: false }],
    ["off", { flag: "disabled" as const }],
    ["off", { scopes: [] }],
    ["pending", { flag: "loading" as const }],
    ["pending", { rbacLoading: true }],
  ])("is %s for %o", (want, patch) => {
    Object.assign(state, patch);
    const { result } = renderHook(() => useConnectionProblemsAccess());
    expect(result.current).toBe(want);
  });
});
