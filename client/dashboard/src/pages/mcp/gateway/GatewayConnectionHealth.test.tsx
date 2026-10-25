import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { GatewayConnectionHealth } from "./GatewayConnectionHealth";

type Payload = Record<string, unknown> & { from: Date; to: Date };

const state = vi.hoisted(() => ({
  read: true,
  health: vi.fn(),
  hotspots: vi.fn(),
  sheet: vi.fn(),
  healthResult: (_payload: Payload): unknown => undefined,
  hotspotsResult: (_payload: Payload): unknown => undefined,
}));
vi.mock("@/components/observe/observeDeepLink", () => ({
  useCanViewToolLogs: () => state.read,
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "default" }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
// The generated keys leave out the body, as the real builders do.
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
vi.mock("@gram/client/react-query/gatewayConnectionHotspots.core.js", () => ({
  buildGatewayConnectionHotspotsQuery: (
    _client: unknown,
    request: { listGatewayConnectionHotspotsPayload: Payload },
  ) => ({
    queryKey: ["hotspots"],
    queryFn: async () => {
      state.hotspots(request.listGatewayConnectionHotspotsPayload);
      return state.hotspotsResult(request.listGatewayConnectionHotspotsPayload);
    },
  }),
}));
vi.mock("@/components/chart/StackedTimeBarChart", () => ({
  StackedTimeBarChart: () => <div>timeline</div>,
}));
vi.mock("./ConnectionHealthSheet", () => ({
  ConnectionHealthSheet: (props: unknown) => {
    state.sheet(props);
    return <div>sheet</div>;
  },
}));

const listing = {
  stage: "discovery_listing",
  attempts: 4,
  outcomes: [
    { outcome: "complete", count: 3 },
    { outcome: "failed", count: 1 },
  ],
  retries: 0,
  recovered: 0,
  buckets: [
    {
      bucketTimeUnixNano: "1767225600000000000",
      outcomes: [{ outcome: "failed", count: 1 }],
    },
  ],
};

function label(text: string, key?: string) {
  return { kind: "user", label: text, key };
}

const visible = {
  subject: label("Amara Example", "user:amara"),
  provider: label("No provider", ""),
  client: label("Registered client", "dcr"),
  attempts: 5,
  failed: 1,
  blocked: 0,
  outcomes: [],
  reasons: [{ reason: "stale_cursor", count: 1 }],
};
const hidden = {
  ...visible,
  subject: label("Unknown person"),
  provider: label("Unknown provider"),
  client: label("Unknown client"),
};
const healthy = {
  ...visible,
  subject: label("Quiet Example", "user:quiet"),
  failed: 0,
  reasons: [],
};

function summary(stages: unknown[] = [listing]) {
  return (payload: Payload) => ({
    from: payload.from,
    to: payload.to,
    intervalSeconds: 3600,
    stages,
    notRecorded: [],
    scope: "",
  });
}

function ranking(hotspots: unknown[]) {
  return (payload: Payload) => ({
    from: payload.from,
    to: payload.to,
    stage: payload.stage,
    rankBy: payload.rankBy,
    minAttempts: payload.minAttempts,
    hotspots,
  });
}

function fail(statusCode?: number) {
  return () => {
    throw Object.assign(new Error("refused"), { statusCode });
  };
}

beforeEach(() => {
  state.read = true;
  state.healthResult = summary();
  state.hotspotsResult = ranking([visible, hidden, hidden, healthy]);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderSection() {
  const client = new QueryClient();
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <GatewayConnectionHealth
          gatewayId="gateway-1"
          onShowConnections={vi.fn<() => void>()}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

it("renders nothing without log access", () => {
  state.read = false;
  const { container } = renderSection();
  expect(container.innerHTML).toBe("");
  expect(state.health).not.toHaveBeenCalled();
});

it("defaults to listings over 7 days, sharing one window", async () => {
  renderSection();
  expect(await screen.findByText("Listings over time")).toBeTruthy();
  const health = state.health.mock.calls[0]![0] as Payload;
  const hotspots = state.hotspots.mock.calls[0]![0] as Payload;
  expect(health.metaMcpServerId).toBe("gateway-1");
  expect(health.to.getTime() - health.from.getTime()).toBe(7 * 24 * 3_600_000);
  expect(hotspots).toMatchObject({
    stage: "discovery_listing",
    rankBy: "count",
    minAttempts: 5,
    from: health.from,
    to: health.to,
  });
  expect(screen.getAllByText("of 4")).toHaveLength(3);
  expect(screen.getByText(/a successful sign-in is not shown/)).toBeTruthy();
});

it("refetches for every control change", async () => {
  renderSection();
  await screen.findByText("Listings over time");
  fireEvent.click(screen.getByRole("button", { name: "Highest failure rate" }));
  await waitFor(() =>
    expect(state.hotspots.mock.lastCall![0]).toMatchObject({ rankBy: "rate" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "20+" }));
  await waitFor(() =>
    expect(state.hotspots.mock.lastCall![0]).toMatchObject({ minAttempts: 20 }),
  );
  fireEvent.click(screen.getByRole("button", { name: "24h" }));
  await waitFor(() => {
    const health = state.health.mock.lastCall![0] as Payload;
    expect(health.to.getTime() - health.from.getTime()).toBe(24 * 3_600_000);
  });
  fireEvent.click(screen.getByRole("button", { name: "Member checks" }));
  await waitFor(() =>
    expect(state.hotspots.mock.lastCall![0]).toMatchObject({
      stage: "discovery_member",
    }),
  );
});

it("opens only visible hotspots, keys identical hidden rows apart and leaves out rows with nothing failing", async () => {
  const errors = vi.spyOn(console, "error").mockImplementation(() => {});
  renderSection();
  await screen.findByText("Amara Example");
  expect(screen.getAllByRole("button", { name: "View" })).toHaveLength(1);
  expect(screen.getAllByText("Not visible to you")).toHaveLength(2);
  expect(screen.queryByText("Quiet Example")).toBeNull();
  expect(
    errors.mock.calls.some((call) => String(call[0]).includes("same key")),
  ).toBe(false);
  errors.mockRestore();
  fireEvent.click(screen.getByRole("button", { name: "View" }));
  expect(state.sheet.mock.lastCall![0]).toMatchObject({
    gatewayId: "gateway-1",
    stage: "discovery_listing",
    hotspot: visible,
  });
});

it("never shows results answered for other controls", async () => {
  state.healthResult = (payload) => ({
    ...summary()(payload),
    from: new Date(payload.from.getTime() - 1),
  });
  state.hotspotsResult = (payload) =>
    ranking([visible])({ ...payload, stage: "discovery_member" });
  renderSection();
  await waitFor(() => expect(state.hotspots).toHaveBeenCalled());
  await new Promise((resolve) => {
    setTimeout(resolve, 0);
  });
  expect(screen.queryByText("Listings over time")).toBeNull();
  expect(screen.queryByText("Amara Example")).toBeNull();
});

it("treats no attempts as unknown, not healthy", async () => {
  state.healthResult = summary([{ ...listing, attempts: 0, outcomes: [] }]);
  renderSection();
  expect(
    await screen.findByText(/No listings were recorded in the last 7 days/),
  ).toBeTruthy();
  expect(screen.queryByText("Listings over time")).toBeNull();
});

it("says when a stage is not recorded", async () => {
  state.healthResult = summary([]);
  renderSection();
  expect(
    await screen.findByText("Listings are not recorded on this server yet."),
  ).toBeTruthy();
});

it("says when health is not available to the caller", async () => {
  state.healthResult = fail(404);
  renderSection();
  expect(
    await screen.findByText(/Connection health is not available to you/),
  ).toBeTruthy();
});

it("offers a retry when the summary or hotspots fail", async () => {
  state.healthResult = fail();
  state.hotspotsResult = fail();
  renderSection();
  expect(
    await screen.findByText("Couldn't load connection health."),
  ).toBeTruthy();
  await screen.findByText("Couldn't load where connections fail.");
  state.healthResult = summary();
  state.hotspotsResult = ranking([visible]);
  for (const button of screen.getAllByRole("button", { name: "Retry" })) {
    fireEvent.click(button);
  }
  expect(await screen.findByText("Listings over time")).toBeTruthy();
  expect(await screen.findByText("Amara Example")).toBeTruthy();
});

it("holds skeletons while loading", () => {
  state.healthResult = () => new Promise(() => {});
  state.hotspotsResult = () => new Promise(() => {});
  renderSection();
  expect(screen.queryByText("Not visible to you")).toBeNull();
  expect(screen.queryByText(/No listings were recorded/)).toBeNull();
  expect(screen.queryByText("Couldn't load connection health.")).toBeNull();
});
