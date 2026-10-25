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
  tokenHealth: vi.fn(),
  latestSearch: vi.fn(),
  healthResult: (_payload: Payload): unknown => undefined,
  tokenResult: (_payload: Payload): unknown => undefined,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
  useProject: () => ({ id: "project-1", slug: "default" }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
// The related sign-in read asks for code_issued rows only; it is heard apart
// from the listing's own attempts.
vi.mock("@gram/client/funcs/telemetrySearchLogs", () => ({
  telemetrySearchLogs: (
    client: unknown,
    request: { searchLogsPayload: { filters: { path: string }[] } },
  ) =>
    request.searchLogsPayload.filters.some(
      (filter) => filter.path === "gram.connection_attempt.outcome",
    )
      ? state.latestSearch(client, request)
      : state.search(client, request),
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
      if (
        (payload.key as { stage?: string } | undefined)?.stage ===
        "inbound_token"
      ) {
        state.tokenHealth(payload);
        return state.tokenResult(payload);
      }
      state.health(payload);
      return state.healthResult(payload);
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

function tokens(payload: Payload) {
  return {
    from: payload.from,
    to: payload.to,
    intervalSeconds: 3600,
    notRecorded: [],
    scope: "",
    stages: [
      {
        stage: "inbound_token",
        attempts: 3,
        outcomes: [
          { outcome: "code_issued", count: 2 },
          { outcome: "refreshed", count: 1 },
        ],
        retries: 0,
        recovered: 0,
        buckets: [],
      },
    ],
  };
}

const LATEST_SIGN_IN_NS = END_NS - 7_200_000_000_000n;

beforeEach(() => {
  state.healthResult = keyed;
  state.tokenResult = tokens;
  state.latestSearch.mockImplementation(() =>
    ok([log("sign-in", LATEST_SIGN_IN_NS, "code_issued")]),
  );
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
const REGISTERED = "6c0d0000-0000-4000-a000-000000000002";
// A listing hotspot whose client is registered, so it can be matched to
// sign-in token responses.
const registered = {
  ...hotspot,
  client: { kind: "registered", label: "Example client", key: REGISTERED },
};
const registeredKey = { ...key, client: REGISTERED };
const showRegistered = (client?: QueryClient) =>
  renderSheet(undefined, client, registered);

it("shows the sign-in token responses written for the same person and client", async () => {
  showRegistered();
  expect(
    await screen.findByText(/The gateway wrote 2 sign-in token responses/),
  ).toBeTruthy();
  expect(await screen.findByText(latestText)).toBeTruthy();
  expect(state.tokenHealth.mock.calls[0]![0]).toMatchObject({
    from,
    to,
    key: { ...registeredKey, stage: "inbound_token" },
  });
  const latest = state.latestSearch.mock.calls[0]![1].searchLogsPayload;
  expect(latest).toMatchObject({ from, to, sort: "desc", limit: 1 });
  expect(latest.filters).toContainEqual({
    path: "gram.connection_attempt.grant_type",
    operator: "eq",
    values: ["authorization_code"],
  });
  expect(
    screen.getByText(
      /Whether the app received or used a token is not observed/,
    ),
  ).toBeTruthy();
});

it("says none is recorded rather than that none was written", async () => {
  state.tokenResult = (payload) => ({
    ...tokens(payload),
    stages: [
      {
        ...tokens(payload).stages[0]!,
        attempts: 1,
        outcomes: [{ outcome: "refreshed", count: 1 }],
      },
    ],
  });
  showRegistered();
  expect(
    await screen.findByText(
      /No sign-in token response is recorded for this person and client/,
    ),
  ).toBeTruthy();
  expect(state.latestSearch).not.toHaveBeenCalled();
});

it("reads no related evidence on reopening until a fresh token read succeeds", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const first = showRegistered(client);
  await screen.findByText(latestText);
  first.unmount();
  const latestReads = state.latestSearch.mock.calls.length;

  const fresh = deferred();
  state.tokenResult = () => fresh.promise;
  showRegistered(client);
  await screen.findByText("Latest 3 attempts in this window");
  await waitFor(() => expect(state.tokenHealth).toHaveBeenCalledTimes(2));
  expect(screen.queryByText(/sign-in token responses/)).toBeNull();
  expect(screen.queryByText(latestText)).toBeNull();
  expect(state.latestSearch.mock.calls.length).toBe(latestReads);

  fresh.reject(Object.assign(new Error("refused"), { statusCode: 403 }));
  expect(
    await screen.findByText(/Couldn't load sign-in token responses/),
  ).toBeTruthy();
  expect(screen.queryByText(latestText)).toBeNull();
  expect(state.latestSearch.mock.calls.length).toBe(latestReads);
});

it("hides an earlier latest time when its refetch fails", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const first = showRegistered(client);
  await screen.findByText(latestText);
  first.unmount();

  state.latestSearch.mockImplementation(() =>
    Promise.reject(Object.assign(new Error("boom"), { statusCode: 500 })),
  );
  showRegistered(client);
  expect(
    await screen.findByText("The latest request time couldn't be loaded."),
  ).toBeTruthy();
  expect(screen.queryByText(latestText)).toBeNull();

  state.latestSearch.mockImplementation(() =>
    ok([log("sign-in", LATEST_SIGN_IN_NS, "code_issued")]),
  );
  fireEvent.click(screen.getAllByRole("button", { name: "Retry" }).at(-1)!);
  expect(await screen.findByText(latestText)).toBeTruthy();
});

it("does not look for sign-in tokens a key cannot be matched to", async () => {
  renderSheet();
  await screen.findByText("Latest 3 attempts in this window");
  expect(
    screen.getByText(
      /can be matched only to a signed-in person using a registered client/,
    ),
  ).toBeTruthy();
  expect(state.tokenHealth).not.toHaveBeenCalled();
  expect(state.latestSearch).not.toHaveBeenCalled();
  expect(
    screen.queryByText(/No sign-in token response is recorded/),
  ).toBeNull();
});
