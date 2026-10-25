import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ActiveLogFilter } from "@/pages/logs/log-filter-types";
import { ConnectionHealthSheet } from "./ConnectionHealthSheet";
import { matchingConnectionFilters } from "./connectionHealth";

type Payload = Record<string, unknown> & { from: Date; to: Date };

const state = vi.hoisted(() => ({
  health: vi.fn(),
  search: vi.fn(),
  callerHealth: vi.fn(),
  providers: vi.fn(),
  healthResult: (_payload: Payload): unknown => undefined,
  callerResult: (_payload: Payload): unknown => undefined,
  providerResult: (_payload: Payload): unknown => undefined,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
  useProject: () => ({ id: "project-1", slug: "default" }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/funcs/telemetrySearchLogs", () => ({
  telemetrySearchLogs: (client: unknown, request: unknown) =>
    state.search(client, request),
}));
// The generated key leaves out the body, as the real builder does.
vi.mock("@gram/client/react-query/gatewayConnectionHealth.core.js", () => ({
  buildGatewayConnectionHealthQuery: (
    _client: unknown,
    request: { getGatewayConnectionHealthPayload: Payload },
  ) => ({
    queryKey: ["health"],
    queryFn: async () => {
      const payload = request.getGatewayConnectionHealthPayload;
      if (payload.caller) {
        state.callerHealth(payload);
        return state.callerResult(payload);
      }
      state.health(payload);
      return state.healthResult(payload);
    },
  }),
}));
vi.mock("@gram/client/react-query/gatewayConnectionHotspots.core.js", () => ({
  buildGatewayConnectionHotspotsQuery: (
    _client: unknown,
    request: { listGatewayConnectionHotspotsPayload: Payload },
  ) => ({
    queryKey: ["hotspots"],
    queryFn: async () => {
      const payload = request.listGatewayConnectionHotspotsPayload;
      state.providers(payload);
      return state.providerResult(payload);
    },
  }),
}));
vi.mock("@/components/chart/StackedTimeBarChart", () => ({
  StackedTimeBarChart: () => <div>timeline</div>,
}));

const from = new Date("2026-01-01T00:00:00.000Z");
const to = new Date("2026-01-08T00:00:00.000Z");
const END_NS = BigInt(to.getTime()) * 1_000_000n;
const USER = "user:amara";
const REGISTERED = "6c0d0000-0000-4000-a000-000000000002";

const hotspot = {
  subject: { kind: "user", label: "Amara Example", key: USER },
  provider: { kind: "none", label: "No provider", key: "" },
  client: { kind: "registered", label: "Example client", key: "dcr" },
  attempts: 3,
  failed: 1,
  blocked: 0,
  outcomes: [],
  reasons: [{ reason: "stale_cursor", count: 1 }],
};
const key = {
  stage: "discovery_listing" as const,
  subject: USER,
  providerId: "",
  client: "dcr",
};

function keyed(payload: Payload) {
  return {
    from: payload.from,
    to: payload.to,
    intervalSeconds: 3600,
    notRecorded: [],
    scope: "",
    stages: [
      {
        stage: "discovery_listing",
        attempts: 2,
        outcomes: [
          { outcome: "complete", count: 1 },
          { outcome: "failed", count: 1 },
        ],
        retries: 0,
        recovered: 0,
        buckets: [],
      },
    ],
  };
}

function refuse(statusCode?: number) {
  return () => {
    throw Object.assign(new Error("refused"), { statusCode });
  };
}

function log(id: string, nanos: bigint, outcome: string, reason = "") {
  return {
    id,
    timeUnixNano: nanos.toString(),
    attributes: { gram: { connection_attempt: { outcome, reason } } },
  };
}

function ok(logs: unknown[], nextCursor?: string) {
  return Promise.resolve({ ok: true, value: { logs, nextCursor } });
}

const LAST_CODE = new Date("2026-01-07T22:00:00.000Z");
const AUTHORIZE_GAP =
  "Sign-in requests are recorded before the caller is known, so they are not counted for one person or agent.";

function stageView(stage: string, outcomes: Record<string, number>) {
  const counts = Object.entries(outcomes).map(([outcome, count]) => ({
    outcome,
    count,
  }));
  return {
    stage,
    attempts: counts.reduce((sum, c) => sum + c.count, 0),
    outcomes: counts,
    retries: 0,
    recovered: 0,
    buckets: [],
  };
}

// The same person and client: two sign-in token responses, a provider
// credential saved with each of two providers, and one refresh.
function callerSummary(payload: Payload) {
  return {
    from: payload.from,
    to: payload.to,
    intervalSeconds: 3600,
    notRecorded: [{ stage: "inbound_authorize", note: AUTHORIZE_GAP }],
    scope: "",
    caller: payload.caller,
    stages: [
      {
        ...stageView("inbound_token", { code_issued: 2, refreshed: 1 }),
        lastCodeIssuedAt: LAST_CODE,
      },
      stageView("upstream_callback", { stored: 2 }),
      stageView("upstream_refresh", { refreshed: 1 }),
      stageView("discovery_listing", { failed: 2, complete: 1 }),
      stageView("discovery_member", {}),
    ],
  };
}

function providerRow(
  label: string,
  id: string,
  outcomes: Record<string, number>,
) {
  const view = stageView("", outcomes);
  return {
    subject: { kind: "user", label: "Amara Example", key: USER },
    provider: { kind: "provider", label, id, key: id },
    client: { kind: "registered", label: "Example client", key: REGISTERED },
    attempts: view.attempts,
    failed: outcomes.failed ?? 0,
    blocked: 0,
    outcomes: view.outcomes,
    reasons: [],
    lastSeenAt: LAST_CODE,
  };
}

const PROVIDER_A = "6c0d0000-0000-4000-a000-0000000000a1";
const PROVIDER_B = "6c0d0000-0000-4000-a000-0000000000b2";

function providerRanking(payload: Payload) {
  const rows =
    payload.stage === "upstream_callback"
      ? [
          providerRow("Example Tracker", PROVIDER_A, { stored: 1 }),
          providerRow("Example Wiki", PROVIDER_B, { stored: 1 }),
        ]
      : [providerRow("Example Tracker", PROVIDER_A, { refreshed: 1 })];
  return {
    from: payload.from,
    to: payload.to,
    stage: payload.stage,
    rankBy: "count",
    minAttempts: 1,
    hotspots: rows,
    truncated: false,
    caller: payload.caller,
  };
}

beforeEach(() => {
  state.healthResult = keyed;
  state.callerResult = callerSummary;
  state.providerResult = providerRanking;
  state.search.mockImplementation(
    (_client: unknown, request: { searchLogsPayload: Payload }) => {
      const payload = request.searchLogsPayload;
      if (payload.sort === "asc") {
        return ok([log("first", END_NS - 3_600_000_000_000n, "failed")]);
      }
      if (payload.cursor === "page-2") {
        return ok([log("older", END_NS - 7_200_000_000_000n, "failed")]);
      }
      // An attempt stamped exactly at the end is outside [from, to).
      return ok(
        [
          log("at-end", END_NS, "complete"),
          log("latest", END_NS - 1n, "complete"),
          log("stale", END_NS - 60_000_000_000n, "failed", "stale_cursor"),
        ],
        "page-2",
      );
    },
  );
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderSheet(
  onShowConnections = vi.fn<(filters: ActiveLogFilter[]) => void>(),
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  shown: typeof hotspot = hotspot,
) {
  // The section's unkeyed summary under the generated key must not be read
  // as this key's counts.
  client.setQueryData(["health"], keyed({ from, to }));
  const { unmount } = render(
    <QueryClientProvider client={client}>
      <ConnectionHealthSheet
        gatewayId="gateway-1"
        stage="discovery_listing"
        hotspot={shown}
        from={from}
        to={to}
        windowPhrase="in the last 7 days"
        onClose={vi.fn<() => void>()}
        onShowConnections={onShowConnections}
      />
    </QueryClientProvider>,
  );
  return { onShowConnections, client, unmount };
}

function deferred() {
  let resolve: (value: unknown) => void = () => {};
  let reject: (error: unknown) => void = () => {};
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

it.each([
  ["the default stale time", 0],
  ["an infinite stale time", Infinity],
])(
  "asks again on reopening with %s and reads no raw attempts until the fresh read succeeds",
  async (_, staleTime) => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime } },
    });
    const first = renderSheet(undefined, client);
    await screen.findByText("Latest 3 attempts in this window");
    first.unmount();
    const searches = state.search.mock.calls.length;
    const reads = state.health.mock.calls.length;

    const fresh = deferred();
    state.healthResult = () => fresh.promise;
    renderSheet(undefined, client);
    await waitFor(() => expect(state.health.mock.calls.length).toBe(reads + 1));
    // The cached answer from the first opening is neither shown nor trusted.
    expect(screen.queryByText("Attempts")).toBeNull();
    expect(screen.queryByText(/Latest \d+ attempts/)).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Show matching connections" }),
    ).toBeNull();
    expect(state.search.mock.calls.length).toBe(searches);

    fresh.reject(Object.assign(new Error("refused"), { statusCode: 403 }));
    expect(
      await screen.findByText(/This connection is not visible to you/),
    ).toBeTruthy();
    expect(state.search.mock.calls.length).toBe(searches);
    expect(
      screen.queryByRole("button", { name: "Show matching connections" }),
    ).toBeNull();
  },
);

it("shows the detail again once the fresh read on reopening succeeds", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const first = renderSheet(undefined, client);
  await screen.findByText("Latest 3 attempts in this window");
  first.unmount();

  const fresh = deferred();
  state.healthResult = () => fresh.promise;
  renderSheet(undefined, client);
  await waitFor(() => expect(state.health).toHaveBeenCalledTimes(2));
  expect(screen.queryByText("Attempts")).toBeNull();
  fresh.resolve(keyed({ from, to }));
  expect(await screen.findByText("Attempts")).toBeTruthy();
  expect(
    await screen.findByRole("button", { name: "Show matching connections" }),
  ).toBeTruthy();
});

it("reads the row's own key over the shared window", async () => {
  renderSheet();
  await waitFor(() => expect(state.health).toHaveBeenCalled());
  expect(state.health.mock.calls[0]![0]).toEqual({
    metaMcpServerId: "gateway-1",
    from,
    to,
    key,
  });
  expect(
    await screen.findByText("Latest 3 attempts in this window"),
  ).toBeTruthy();
  expect(screen.getByText("50% of 2")).toBeTruthy();
});

it("drops the attempt at the window end and refills from the next page", async () => {
  renderSheet();
  await screen.findByText("Latest 3 attempts in this window");
  const pages = state.search.mock.calls
    .map((call) => call[1].searchLogsPayload)
    .filter((payload) => payload.sort === "desc");
  expect(pages).toHaveLength(2);
  expect(pages[0]).toMatchObject({ from, to, limit: 20 });
  expect(pages[1]).toMatchObject({ cursor: "page-2", to });
  expect(pages[0].filters).toContainEqual({
    path: "gram.connection_attempt.provider_id",
    operator: "eq",
    values: [""],
  });
  expect(screen.getAllByText("Complete")).toHaveLength(1);
  // Once under "Why it fails" and once on the failed attempt.
  expect(
    screen.getAllByText(
      /The client asked for a later page of an earlier listing/,
    ),
  ).toHaveLength(2);
  expect(screen.getByText(/First attempt in this window/)).toBeTruthy();
});

it("refuses the whole detail when the key is not visible", async () => {
  state.healthResult = refuse(404);
  renderSheet();
  expect(
    await screen.findByText(/This connection is not visible to you/),
  ).toBeTruthy();
  expect(state.search).not.toHaveBeenCalled();
  expect(screen.queryByText("Why it fails")).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Show matching connections" }),
  ).toBeNull();
});

it("hides earlier keyed data once a refetch is refused", async () => {
  const { client } = renderSheet();
  await screen.findByText("Latest 3 attempts in this window");
  state.healthResult = refuse(403);
  state.search.mockClear();
  await client.refetchQueries();
  expect(
    await screen.findByText(/This connection is not visible to you/),
  ).toBeTruthy();
  expect(screen.queryByText("Attempts")).toBeNull();
  expect(screen.queryByText("Why it fails")).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Show matching connections" }),
  ).toBeNull();
});

it("retries the keyed read when it fails", async () => {
  state.healthResult = refuse();
  renderSheet();
  const retry = await screen.findByRole("button", { name: "Retry" });
  expect(state.search).not.toHaveBeenCalled();
  state.healthResult = keyed;
  fireEvent.click(retry);
  expect(
    await screen.findByText("Latest 3 attempts in this window"),
  ).toBeTruthy();
});

it("narrows connections by exact matches only and says what it cannot match", async () => {
  const { onShowConnections: onShow } = renderSheet();
  await screen.findByText("Latest 3 attempts in this window");
  expect(screen.getByText(/Narrows by this caller only/)).toBeTruthy();
  fireEvent.click(
    screen.getByRole("button", { name: "Show matching connections" }),
  );
  await waitFor(() => expect(onShow).toHaveBeenCalledTimes(1));
  const chips = onShow.mock.calls[0]![0] as { path: string; value: string }[];
  expect(chips.map(({ path, value }) => [path, value])).toEqual(
    matchingConnectionFilters(key).filters.map(({ path, value }) => [
      path,
      value,
    ]),
  );
  expect(screen.queryByRole("button", { name: /revoke/i })).toBeNull();
});

const latestText = /The latest sign-in code request started/;
// A listing hotspot whose client is registered, so its person and client can
// be counted on their own.
const registered = {
  ...hotspot,
  client: { kind: "registered", label: "Example client", key: REGISTERED },
};
const caller = { subject: USER, client: REGISTERED };
const showRegistered = (client?: QueryClient) =>
  renderSheet(undefined, client, registered);
// The listing's own attempt reads: two pages and the first attempt.
const listingReads = 3;

it("shows the same person and client's token responses and each provider's sign-ins and refreshes, successes included", async () => {
  showRegistered();
  expect(
    await screen.findByText(/The gateway wrote 2 sign-in token responses/),
  ).toBeTruthy();
  expect(screen.getByText(latestText)).toBeTruthy();
  expect(await screen.findByText("Example Wiki")).toBeTruthy();
  expect(screen.getAllByText("Example Tracker")).toHaveLength(2);
  expect(screen.getAllByText(/^Credentials saved: 1/)).toHaveLength(2);
  expect(screen.getAllByText(/^Refreshed: 1/)).toHaveLength(1);
  expect(state.callerHealth.mock.calls[0]![0]).toEqual({
    metaMcpServerId: "gateway-1",
    from,
    to,
    caller,
  });
  const stages = state.providers.mock.calls.map((call) => call[0]);
  expect(
    stages
      .map((payload) => String(payload.stage))
      .sort((a, b) => a.localeCompare(b)),
  ).toEqual(["upstream_callback", "upstream_refresh"]);
  for (const payload of stages) {
    expect(payload).toMatchObject({
      from,
      to,
      caller,
      minAttempts: 1,
      limit: 25,
    });
  }
  // The latest code time comes from the summary: no extra raw read.
  expect(state.search).toHaveBeenCalledTimes(listingReads);
  expect(
    screen.getByText(/These events match this person, client and time window/),
  ).toBeTruthy();
  // The caller-specific gap the server reported, and none of the shared ones.
  expect(screen.getByText(AUTHORIZE_GAP)).toBeTruthy();
});

it("says how many attempts the listed providers leave out", async () => {
  state.callerResult = (payload) => ({
    ...callerSummary(payload),
    stages: callerSummary(payload).stages.map((stage) =>
      stage.stage === "upstream_callback"
        ? stageView("upstream_callback", { stored: 3, failed: 2 })
        : stage,
    ),
  });
  state.providerResult = (payload) => ({
    ...providerRanking(payload),
    truncated: payload.stage === "upstream_callback",
  });
  showRegistered();
  expect(
    await screen.findByText(
      "3 more attempts are recorded with providers not listed here.",
    ),
  ).toBeTruthy();
});

it("says nothing is recorded, not that nothing happened, and reads no providers then", async () => {
  state.callerResult = (payload) => ({
    ...callerSummary(payload),
    stages: [
      stageView("inbound_token", {}),
      stageView("upstream_callback", {}),
      stageView("upstream_refresh", {}),
    ],
  });
  showRegistered();
  expect(
    await screen.findByText(
      /No provider sign-in is recorded for this person and client in the last 7 days\. That is no evidence either way\./,
    ),
  ).toBeTruthy();
  expect(
    screen.getByText(
      "No provider refresh caused by a request was recorded for this person and client in the last 7 days. That is no evidence either way.",
    ),
  ).toBeTruthy();
  expect(
    screen.getByText(
      /No sign-in token response is recorded for this person and client/,
    ),
  ).toBeTruthy();
  expect(state.providers).not.toHaveBeenCalled();
});

it("reads no provider rows on reopening until a fresh caller read succeeds, and hides them when it is refused", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const first = showRegistered(client);
  await screen.findByText("Example Wiki");
  first.unmount();
  const providerReads = state.providers.mock.calls.length;

  const fresh = deferred();
  state.callerResult = () => fresh.promise;
  showRegistered(client);
  await screen.findByText("Latest 3 attempts in this window");
  await waitFor(() => expect(state.callerHealth).toHaveBeenCalledTimes(2));
  expect(screen.queryByText("Example Wiki")).toBeNull();
  expect(screen.queryByText(latestText)).toBeNull();
  expect(screen.queryByText(AUTHORIZE_GAP)).toBeNull();
  expect(state.providers.mock.calls.length).toBe(providerReads);

  fresh.reject(Object.assign(new Error("refused"), { statusCode: 403 }));
  expect(
    await screen.findByText(/This person or client isn't visible to you now/),
  ).toBeTruthy();
  expect(screen.queryByText("Example Wiki")).toBeNull();
  expect(screen.queryByText(AUTHORIZE_GAP)).toBeNull();
  expect(state.providers.mock.calls.length).toBe(providerReads);
});

it("hides earlier provider rows when a provider read on reopening is refused", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const first = showRegistered(client);
  await screen.findByText("Example Wiki");
  first.unmount();

  state.providerResult = refuse(404);
  showRegistered(client);
  await waitFor(() =>
    expect(
      screen.getAllByText(
        /isn't visible to you now, so their providers are not shown/,
      ),
    ).toHaveLength(2),
  );
  expect(screen.queryByText("Example Wiki")).toBeNull();
  expect(screen.queryByText("Example Tracker")).toBeNull();
});

it("does not show a caller answer for another window", async () => {
  state.callerResult = (payload) => ({
    ...callerSummary(payload),
    from: new Date("2025-12-01T00:00:00.000Z"),
  });
  showRegistered();
  await screen.findByText("Latest 3 attempts in this window");
  await waitFor(() => expect(state.callerHealth).toHaveBeenCalled());
  expect(screen.queryByText(/sign-in token responses/)).toBeNull();
  expect(state.providers).not.toHaveBeenCalled();
});

it("shows no caller facts from a server that answered for the whole gateway", async () => {
  state.callerResult = (payload) => ({
    ...callerSummary(payload),
    caller: undefined,
  });
  showRegistered();
  expect(
    await screen.findByText(
      /This server can't count one person and client on their own yet/,
    ),
  ).toBeTruthy();
  expect(screen.queryByText(/sign-in token responses/)).toBeNull();
  expect(screen.queryByText(AUTHORIZE_GAP)).toBeNull();
  expect(state.providers).not.toHaveBeenCalled();
});

it("shows no provider rows a server did not scope to this caller", async () => {
  state.providerResult = (payload) => ({
    ...providerRanking(payload),
    caller: { subject: "user:someone-else", client: REGISTERED },
  });
  showRegistered();
  await waitFor(() =>
    expect(
      screen.getAllByText(
        /This server can't list one person and client's providers yet/,
      ),
    ).toHaveLength(2),
  );
  expect(screen.queryByText("Example Wiki")).toBeNull();
});

it("lists hidden providers that share a label as separate rows", async () => {
  state.providerResult = (payload) => {
    const hiddenRow = {
      ...providerRow("Unknown provider", "", { stored: 1 }),
      provider: { kind: "provider", label: "Unknown provider" },
    };
    return {
      ...providerRanking(payload),
      hotspots:
        payload.stage === "upstream_callback" ? [hiddenRow, hiddenRow] : [],
    };
  };
  showRegistered();
  await waitFor(() =>
    expect(screen.getAllByText("Unknown provider")).toHaveLength(2),
  );
});

it("does not count a caller a key cannot be matched to", async () => {
  renderSheet();
  await screen.findByText("Latest 3 attempts in this window");
  expect(
    screen.getByText(
      /can be matched only to a signed-in person or agent using a registered client/,
    ),
  ).toBeTruthy();
  expect(state.callerHealth).not.toHaveBeenCalled();
  expect(state.providers).not.toHaveBeenCalled();
});

it("retries failed caller and provider reads without showing cached facts", async () => {
  state.callerResult = refuse(500);
  showRegistered();
  await screen.findByText(
    "Couldn't load what the gateway recorded for this person and client.",
  );
  expect(state.providers).not.toHaveBeenCalled();
  state.callerResult = callerSummary;
  state.providerResult = refuse(500);
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(
    await screen.findByText(/The gateway wrote 2 sign-in token responses/),
  ).toBeTruthy();
  await waitFor(() =>
    expect(screen.getAllByRole("button", { name: "Retry" })).toHaveLength(2),
  );
  const retries = screen.getAllByRole("button", { name: "Retry" });
  state.providerResult = providerRanking;
  fireEvent.click(retries[0]!);
  expect(await screen.findByText("Example Wiki")).toBeTruthy();
});
