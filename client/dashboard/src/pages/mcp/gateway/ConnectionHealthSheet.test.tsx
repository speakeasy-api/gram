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
  healthResult: (_payload: Payload): unknown => undefined,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
  useProject: () => ({ id: "project-1", slug: "default" }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/funcs/telemetrySearchLogs", () => ({
  telemetrySearchLogs: state.search,
}));
// The generated key leaves out the body, as the real builder does.
vi.mock("@gram/client/react-query/gatewayConnectionHealth.core.js", () => ({
  buildGatewayConnectionHealthQuery: (
    _client: unknown,
    request: { getGatewayConnectionHealthPayload: Payload },
  ) => ({
    queryKey: ["health"],
    queryFn: async () => {
      state.health(request.getGatewayConnectionHealthPayload);
      return state.healthResult(request.getGatewayConnectionHealthPayload);
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

beforeEach(() => {
  state.healthResult = keyed;
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
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  // The section's unkeyed summary under the generated key must not be read
  // as this key's counts.
  client.setQueryData(["health"], keyed({ from, to }));
  render(
    <QueryClientProvider client={client}>
      <ConnectionHealthSheet
        gatewayId="gateway-1"
        stage="discovery_listing"
        hotspot={hotspot}
        from={from}
        to={to}
        windowPhrase="in the last 7 days"
        onClose={vi.fn<() => void>()}
        onShowConnections={onShowConnections}
      />
    </QueryClientProvider>,
  );
  return { onShowConnections, client };
}

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
