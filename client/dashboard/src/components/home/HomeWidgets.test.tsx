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
import type { ProjectSuspectedProblem } from "@gram/client/models/components/projectsuspectedproblem.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { formatSince } from "@/pages/mcp-sessions/connection-health/format";

const state = vi.hoisted(() => ({
  flag: "enabled" as "enabled" | "disabled" | "loading",
  scopes: ["log:read"] as string[],
  rbacLoading: false,
  logsEnabled: true,
  projectId: "project-1",
  projectSlug: "project",
  // The project the viewer's log:read grant is on.
  logReadProject: "project-1",
  hold: null as null | { release: () => void },
  connections: {
    ok: true,
    loggingEnabled: true,
    viewerScope: "all",
    problems: [] as ProjectSuspectedProblem[],
    total: undefined as number | undefined,
    status: 503,
  },
  connectionCalls: [] as unknown[],
  failingTools: [] as Record<string, unknown>[],
  weekFails: false,
  signals: [] as Record<string, unknown>[],
  signalsFail: false,
}));

const unavailable = (status = 503) =>
  new GramError("unavailable", {
    response: new Response("", { status }),
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
      const answer = () =>
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
          : { ok: false, error: unavailable(c.status) };
      if (!state.hold) return Promise.resolve(answer());
      return new Promise((resolve) => {
        state.hold = { release: () => resolve(answer()) };
      });
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
      (scope === "org:admin" || resourceId === state.logReadProject),
    isLoading: state.rbacLoading,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
  useProject: () => ({ id: state.projectId, slug: state.projectSlug }),
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
import { useConnectionProblems } from "./useConnectionProblems";

const NOW = new Date(2026, 10, 20, 12, 0);
const today = (h: number, m: number) => new Date(2026, 10, 20, h, m);

// The SDK's parsed shape: it turns the API's ISO strings into Dates.
function problem(
  id: string,
  extra: Partial<ProjectSuspectedProblem> = {},
): ProjectSuspectedProblem {
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

function renderWidgets(): { rerender: () => void; client: QueryClient } {
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
  return { rerender: () => view.rerender(tree()), client };
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
  state.projectSlug = "project";
  state.logReadProject = "project-1";
  state.hold = null;
  state.connections = {
    ok: true,
    loggingEnabled: true,
    viewerScope: "all",
    problems: [],
    total: undefined,
    status: 503,
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
    state.logReadProject = "project-a";
    state.projectId = "project-b";
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
    state.projectSlug = "project-b";
    state.logReadProject = "project-2";
    state.connections.problems = [problem("b")];
    view.rerender();
    await waitFor(() =>
      expect(rows().map((l) => l.textContent)).toEqual([
        expect.stringContaining("Problem b"),
      ]),
    );
    expect(state.connectionCalls).toHaveLength(2);
    expect(state.connectionCalls[1]).toEqual(
      expect.objectContaining({ gramProject: "project-b" }),
    );
  });

  it("keeps the skeleton until a slow read answers", async () => {
    state.hold = { release: () => undefined };
    state.failingTools = [failingTool];
    state.connections.problems = [problem("a")];
    renderWidgets();
    await waitFor(() => expect(state.connectionCalls).toHaveLength(1));
    // Let every other source settle while the read is held.
    await waitFor(() =>
      expect(
        within(
          screen.getByRole("region", { name: "Most active" }),
        ).queryAllByTestId("skeleton"),
      ).toHaveLength(0),
    );
    await act(async () => {});
    expect(within(card()).getAllByTestId("skeleton").length).toBeGreaterThan(0);
    expect(within(card()).queryByText("search is failing")).toBeNull();
    await act(async () => {
      state.hold?.release();
    });
    await settled();
    expect(within(card()).getAllByRole("listitem")[0]!.textContent).toContain(
      "Problem a",
    );
  });

  it("drops rows from a read that later fails", async () => {
    state.connections.problems = [problem("a")];
    state.connections.total = 9;
    const { client } = renderWidgets();
    await settled();
    expect(rows().length).toBeGreaterThan(0);
    state.connections.ok = false;
    await act(async () => {
      await client.refetchQueries({
        queryKey: ["mcpConnectionHealth", "project-1"],
      });
    });
    await waitFor(() =>
      expect(within(card()).getByText(LOAD_ERROR)).toBeTruthy(),
    );
    expect(rows()).toHaveLength(0);
    expect(within(card()).queryByRole("link", { name: "View all" })).toBeNull();
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
    expect(card().getAttribute("data-tooltip")).toBe(
      "High-risk signals, failing tools, and pending access requests from the last 7 days.",
    );
  });

  it("treats a not-found read as connection health off, not as a failure", async () => {
    state.scopes = ["log:read", "org:admin"];
    state.signals = ["1", "2", "3"].map((id) => ({
      ...criticalSignal,
      ruleId: `rule-${id}`,
      description: `Signal ${id}`,
    }));
    state.failingTools = ["a", "b", "c"].map((t) => ({
      ...failingTool,
      gramUrn: `tools:http:x:${t}`,
    }));
    state.connections.ok = false;
    state.connections.status = 404;
    renderWidgets();
    await settled();
    expect(within(card()).getAllByRole("listitem")).toHaveLength(5);
    expect(within(card()).queryByText(PARTIAL)).toBeNull();
    expect(card().getAttribute("data-tooltip")).toBe(
      "High-risk signals, failing tools, and pending access requests from the last 7 days.",
    );
    await act(async () => {});
    expect(state.connectionCalls).toHaveLength(1);
  });

  it("wraps every label to two lines in the tall card and gives problem rows no hint", async () => {
    state.scopes = ["log:read", "org:admin"];
    state.signals = [criticalSignal];
    state.failingTools = [failingTool];
    state.connections.problems = [problem("a")];
    renderWidgets();
    await settled();
    for (const title of [
      /^Problem a since/,
      "Agent reads secrets",
      "search is failing",
    ]) {
      const label = within(card()).getByTitle(title);
      expect(label.className.split(" ")).toEqual(
        expect.arrayContaining(["line-clamp-2", "break-words"]),
      );
      expect(label.className).not.toContain("lg:truncate");
    }
    const [problemRow, signalRow] = rows();
    expect(within(problemRow!).queryByText("Open ›")).toBeNull();
    expect(within(signalRow!).getByText("Open ›").className).toContain(
      "shrink-0",
    );
    expect(within(card()).getByText("Ask ›").className).toContain("shrink-0");
  });

  it("sets the headline in medium weight and keeps since and its time together", async () => {
    state.scopes = ["log:read", "org:admin"];
    state.signals = [criticalSignal];
    state.connections.problems = [
      problem("a"),
      problem("b", {
        suspectedProblemRule: "one_person",
        since: new Date(2026, 10, 19, 16, 5),
      }),
    ];
    renderWidgets();
    await settled();
    const since = `since ${formatSince(today(9, 5).toISOString(), NOW)}`;
    const label = within(card()).getByTitle(`Problem a ${since}`);
    const headline = within(label).getByText("Problem a");
    expect(headline.className).toContain("font-medium");
    const when = within(label).getByText(since);
    expect(when.className).toContain("whitespace-nowrap");
    expect(when.className).not.toContain("font-medium");
    const oneDay = within(card()).getByTitle(/^Problem b since /);
    const oneDaySince = within(oneDay).getByText(/^since /);
    expect(oneDaySince.className).toContain("whitespace-nowrap");
    expect(oneDaySince.textContent).not.toContain("16:05");
    const own = within(card()).getByTitle("Agent reads secrets");
    expect(own.querySelector(".font-medium")).toBeNull();
    expect(own.textContent).toBe("Agent reads secrets");
  });

  it("keeps main's single-line labels and in-flow hints in the base card", async () => {
    state.flag = "disabled";
    state.failingTools = [failingTool];
    renderWidgets();
    await settled();
    const own = within(card()).getByTitle("search is failing");
    expect(own.className.split(" ")).toEqual(
      expect.arrayContaining([
        "lg:block",
        "lg:truncate",
        "max-lg:line-clamp-2",
      ]),
    );
    expect(within(card()).getByText("Ask ›").className).toContain("shrink-0");
  });

  it("keeps the five-row card when the API says the viewer reads only their own logs", async () => {
    state.scopes = ["log:read", "org:admin"];
    state.signals = ["1", "2", "3"].map((id) => ({
      ...criticalSignal,
      ruleId: `rule-${id}`,
      description: `Signal ${id}`,
    }));
    state.failingTools = ["a", "b", "c"].map((t) => ({
      ...failingTool,
      gramUrn: `tools:http:x:${t}`,
    }));
    state.connections.viewerScope = "own";
    renderWidgets();
    await settled();
    expect(within(card()).getAllByRole("listitem")).toHaveLength(5);
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
    state.failingTools = [failingTool];
    state.connections.problems = ["1", "2", "3", "4", "5", "6"].map((id) =>
      problem(id),
    );
    renderWidgets();
    await settled();
    const items = within(card())
      .getAllByRole("listitem")
      .map((li) => li.textContent ?? "");
    expect(items).toHaveLength(6);
    expect(items.slice(0, 4)).toEqual(
      ["1", "2", "3", "4"].map((id) =>
        expect.stringContaining(`Problem ${id} since`),
      ),
    );
    expect(items[4]).toContain("Agent reads secrets");
    expect(items[5]).toContain("search is failing");
    const viewAll = within(card()).getByRole("link", { name: "View all" });
    expect(viewAll.closest("ul")).toBeNull();
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

describe("useConnectionProblems display", () => {
  it.each([
    ["tall", "an authorized answer", {}, {}],
    ["tall", "an authorized answer with no problems", {}, { problems: [] }],
    ["base", "an own-logs answer", {}, { viewerScope: "own" }],
    ["base", "a logging-off answer", {}, { loggingEnabled: false }],
    ["tall", "a failed read", {}, { ok: false }],
    ["base", "a not-found read", {}, { ok: false, status: 404 }],
    ["base", "logs off", { logsEnabled: false }, {}],
    ["base", "the flag off", { flag: "disabled" as const }, {}],
    ["base", "no log:read", { scopes: [] }, {}],
    ["pending", "the flag loading", { flag: "loading" as const }, {}],
    ["pending", "permissions loading", { rbacLoading: true }, {}],
  ])("is %s for %s", async (want, _name, patch, answer) => {
    Object.assign(state, patch);
    Object.assign(state.connections, { problems: [problem("a")] }, answer);
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result } = renderHook(() => useConnectionProblems(6), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    });
    await waitFor(() =>
      expect(result.current.pending).toBe(want === "pending"),
    );
    expect(result.current.display).toBe(want);
  });

  it("is tall while an eligible viewer's first read is in flight", async () => {
    state.hold = { release: () => undefined };
    const client = new QueryClient();
    const { result } = renderHook(() => useConnectionProblems(6), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    });
    await act(async () => {});
    expect(result.current.pending).toBe(true);
    expect(result.current.display).toBe("tall");
  });
});
