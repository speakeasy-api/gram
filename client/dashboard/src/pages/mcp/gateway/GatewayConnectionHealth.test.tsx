import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { GatewayConnectionHealth } from "./GatewayConnectionHealth";
import { healthWindow, type HealthWindow } from "./connectionHealth";

type Payload = Record<string, unknown> & { from: Date; to: Date };

const state = vi.hoisted(() => ({
  read: true,
  health: vi.fn(),
  hotspots: vi.fn(),
  sheet: vi.fn(),
  healthResult: (_payload: Payload): unknown => undefined,
  hotspotsResult: (_payload: Payload): unknown => undefined,
}));
// Mirrors the real builder's canonical identity path; its org:read gate is
// IdentityLink's own concern.
vi.mock("@/lib/useIdentityHref", async () => {
  const { encodeIdentityUrn, identityUrnFor } =
    await import("@/lib/identity-urn");
  return {
    useIdentityHrefBuilder:
      () => (ref: Parameters<typeof identityUrnFor>[0] | null) =>
        ref
          ? `/default/identities/${encodeIdentityUrn(identityUrnFor(ref))}`
          : null,
  };
});
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
  vi.restoreAllMocks();
});

// The Debug tab owns the window; the section reads the span and the from/to
// snapshot it is given.
function section(client: QueryClient, span: HealthWindow) {
  const { from, to } = healthWindow(span, new Date());
  return (
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <GatewayConnectionHealth
            gatewayId="gateway-1"
            span={span}
            from={from}
            to={to}
            onShowConnections={vi.fn<() => void>()}
          />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
}

function renderSection() {
  const client = new QueryClient();
  return { client, ...render(section(client, "7d")) };
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
  expect(screen.getAllByText("of 4")).toHaveLength(2);
  expect(screen.getByText("25% of 4")).toBeTruthy();
  expect(screen.getByText(/not whether a redirect arrived/)).toBeTruthy();
});

it("refetches for every control change and for the window it is given", async () => {
  const { client, rerender } = renderSection();
  await screen.findByText("Listings over time");
  fireEvent.click(screen.getByRole("button", { name: "Highest failure rate" }));
  await waitFor(() =>
    expect(state.hotspots.mock.lastCall![0]).toMatchObject({ rankBy: "rate" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "20+" }));
  await waitFor(() =>
    expect(state.hotspots.mock.lastCall![0]).toMatchObject({ minAttempts: 20 }),
  );
  rerender(section(client, "24h"));
  await waitFor(() => {
    const health = state.health.mock.lastCall![0] as Payload;
    expect(health.to.getTime() - health.from.getTime()).toBe(24 * 3_600_000);
  });
  await chooseStage("Member checks");
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
  fireEvent.click(screen.getByRole("button", { name: "View" }));
  expect(state.sheet.mock.lastCall![0]).toMatchObject({
    gatewayId: "gateway-1",
    stage: "discovery_listing",
    hotspot: visible,
  });
});

it("keeps a caller whose listings were only partial, without counting them as failed", async () => {
  const partialOnly = {
    ...visible,
    subject: label("Partial Example", "user:partial"),
    attempts: 6,
    failed: 0,
    outcomes: [
      { outcome: "complete", count: 4 },
      { outcome: "partial", count: 2 },
    ],
    reasons: [],
  };
  const complete = {
    ...healthy,
    outcomes: [{ outcome: "complete", count: 5 }],
  };
  state.hotspotsResult = ranking([partialOnly, complete]);
  renderSection();
  expect(await screen.findByText("Partial Example")).toBeTruthy();
  expect(screen.queryByText("Quiet Example")).toBeNull();
  // The stage's Partial tile and the column header each explain it.
  expect(screen.getAllByRole("button", { name: "About Partial" })).toHaveLength(
    2,
  );
  expect(screen.getByText("0 of 6")).toBeTruthy();
  expect(screen.getByText("2")).toBeTruthy();
  expect(screen.getByText("0%")).toBeTruthy();
  expect(
    screen.getByText(/^1 with failed or partial listings, ranked by failures/),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "View" }));
  expect(state.sheet.mock.lastCall![0]).toMatchObject({
    stage: "discovery_listing",
    hotspot: partialOnly,
  });
});

it("leaves partial-looking rows out on a stage that has no partial outcome", async () => {
  const member = {
    ...healthy,
    outcomes: [{ outcome: "partial", count: 1 }],
  };
  state.hotspotsResult = ranking([member]);
  renderSection();
  await screen.findByText("Listings over time");
  await chooseStage("Member checks");
  expect(
    await screen.findByText(/Nothing failed in the last 7 days/),
  ).toBeTruthy();
  expect(screen.queryByText("Quiet Example")).toBeNull();
  expect(screen.queryByRole("button", { name: "About Partial" })).toBeNull();
});

it("links visible people and agents, marks known clients, and keeps hidden rows inert", async () => {
  const agentId = "0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e0b";
  const person = {
    ...visible,
    subject: {
      kind: "user",
      label: "Amara Example",
      id: "user:amara",
      key: "user:amara",
    },
    client: {
      kind: "registered",
      label: "Claude Code",
      id: "dcr-1",
      key: "dcr-1",
    },
  };
  const agent = {
    ...visible,
    subject: {
      kind: "agent",
      label: "Release Agent",
      id: `agent:${agentId}`,
      key: `agent:${agentId}`,
    },
    client: {
      kind: "unregistered",
      label: "Unregistered client",
      key: "unregistered_client",
    },
  };
  const unknown = {
    ...visible,
    subject: { kind: "user", label: "Unknown person" },
    client: { kind: "unknown", label: "Unknown client" },
  };
  state.hotspotsResult = ranking([person, agent, unknown]);
  renderSection();
  const amara = await screen.findByRole("link", { name: "Amara Example" });
  expect(amara.getAttribute("href")).toBe("/default/identities/user%3Aamara");
  expect(
    screen.getByRole("link", { name: "Release Agent" }).getAttribute("href"),
  ).toBe(`/default/identities/agent%3A${agentId}`);
  expect(screen.getAllByRole("link")).toHaveLength(2);
  expect(screen.getByText("Unknown person").closest("a")).toBeNull();

  const mark = (client: string) =>
    screen.getByText(client).parentElement!.querySelector("svg")!;
  expect(mark("Claude Code").classList.contains("lucide-globe")).toBe(false);
  expect(mark("Unregistered client").classList.contains("lucide-globe")).toBe(
    true,
  );
  expect(mark("Unknown client").classList.contains("lucide-globe")).toBe(true);

  // A name link leaves the row's View action to open the sheet on its own.
  fireEvent.click(amara);
  expect(state.sheet).not.toHaveBeenCalled();
  fireEvent.click(screen.getAllByRole("button", { name: "View" })[0]!);
  expect(state.sheet.mock.lastCall![0]).toMatchObject({ hotspot: person });
  expect(screen.getAllByRole("button", { name: "View" })).toHaveLength(2);
  expect(screen.getAllByText("Not visible to you")).toHaveLength(1);
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

it("names provider stages as recorded and lists what is not counted apart", async () => {
  state.healthResult = (payload: Payload) => ({
    ...summary()(payload),
    notRecorded: [
      {
        stage: "upstream_refresh_background",
        note: "Scheduled and manual provider refreshes are not attributed to a gateway.",
      },
    ],
  });
  renderSection();
  expect(await screen.findByText(/connecting providers/)).toBeTruthy();
  expect(screen.queryByText(/provider sign-ins, and/)).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: /What isn.t counted/ }));
  expect(
    await screen.findByText(
      "Scheduled and manual provider refreshes are not attributed to a gateway.",
    ),
  ).toBeTruthy();
  expect(
    screen.getByText("For tool listings, only Direct listings are recorded."),
  ).toBeTruthy();
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

it("replaces earlier results with the retry state when a refetch fails", async () => {
  const { client } = renderSection();
  await screen.findByText("Listings over time");
  await screen.findByText("Amara Example");
  state.healthResult = fail();
  state.hotspotsResult = fail();
  await client.refetchQueries();
  expect(
    await screen.findByText("Couldn't load connection health."),
  ).toBeTruthy();
  expect(
    await screen.findByText("Couldn't load where connections fail."),
  ).toBeTruthy();
  // The cache still holds the earlier results; none of them is shown.
  expect(
    client.getQueriesData({ queryKey: ["health"] }).some(([, data]) => data),
  ).toBe(true);
  expect(screen.queryByText("Listings over time")).toBeNull();
  expect(screen.queryAllByText("of 4")).toHaveLength(0);
  expect(screen.queryByText("Amara Example")).toBeNull();
});

it("asks for 25 combinations and says when the shown rows come from that cap", async () => {
  state.hotspotsResult = ranking([visible, ...Array(24).fill(healthy)]);
  renderSection();
  expect(
    await screen.findByText(
      /1 with failed or partial listings among the top 25 combinations ranked by failures/,
    ),
  ).toBeTruthy();
  expect(state.hotspots.mock.lastCall![0]).toMatchObject({ limit: 25 });
});

it.each([
  ["Any", ""],
  ["5+", ", with at least 5 listings each"],
  ["20+", ", with at least 20 listings each"],
])("keeps the %s floor in the capped empty state", async (floor, suffix) => {
  state.hotspotsResult = ranking(Array(25).fill(healthy));
  renderSection();
  fireEvent.click(screen.getByRole("button", { name: floor }));
  expect(
    await screen.findByText(
      `None of the top 25 combinations ranked by failures had failed or partial listings in the last 7 days${suffix}.`,
    ),
  ).toBeTruthy();
  expect(screen.queryByText(/Nothing failed/)).toBeNull();
});

it("does not mention a cap below it", async () => {
  renderSection();
  expect(
    await screen.findByText(
      /^3 with failed or partial listings, ranked by failures/,
    ),
  ).toBeTruthy();
  expect(screen.queryByText(/top 25/)).toBeNull();
});

it("heads the member blocked column briefly and explains it in a tooltip", async () => {
  renderSection();
  await screen.findByText("Listings over time");
  await chooseStage("Member checks");
  expect(
    await screen.findByRole("button", { name: "About Blocked" }),
  ).toBeTruthy();
  expect(screen.getByText("Blocked")).toBeTruthy();
  expect(screen.queryByText("Needs sign-in or consent")).toBeNull();
});

it("holds skeletons while loading", () => {
  state.healthResult = () => new Promise(() => {});
  state.hotspotsResult = () => new Promise(() => {});
  const { container } = renderSection();
  // Four summary tiles plus the hotspot table.
  expect(container.querySelectorAll(".skeleton").length).toBeGreaterThan(4);
  expect(screen.queryByText("Not visible to you")).toBeNull();
  expect(screen.queryByText(/No listings were recorded/)).toBeNull();
  expect(screen.queryByText("Couldn't load connection health.")).toBeNull();
});

async function chooseStage(label: string) {
  const user = userEvent.setup();
  await user.click(screen.getByRole("combobox", { name: "Connection stage" }));
  await user.click(screen.getByRole("option", { name: label }));
}

it("offers every recorded stage in one compact control, listings first by default", async () => {
  renderSection();
  await screen.findByText("Listings over time");
  const trigger = screen.getByRole("combobox", { name: "Connection stage" });
  expect(trigger.textContent).toContain("Listings");
  await userEvent.setup().click(trigger);
  expect(
    screen.getAllByRole("option").map((option) => option.textContent),
  ).toEqual([
    "Sign-in requests",
    "Token requests",
    "Provider sign-ins",
    "Provider refreshes",
    "Listings",
    "Member checks",
  ]);
});

it("counts token requests by grant, apart from policy blocks", async () => {
  state.healthResult = summary([
    listing,
    {
      stage: "inbound_token",
      attempts: 5,
      outcomes: [
        { outcome: "code_issued", count: 2 },
        { outcome: "refreshed", count: 1 },
        { outcome: "refresh_replayed", count: 1 },
        { outcome: "policy_blocked", count: 1 },
      ],
      retries: 0,
      recovered: 0,
      buckets: [],
    },
  ]);
  renderSection();
  await screen.findByText("Listings over time");
  await chooseStage("Token requests");
  await waitFor(() =>
    expect(state.hotspots.mock.lastCall![0]).toMatchObject({
      stage: "inbound_token",
    }),
  );
  const tile = (title: string) =>
    screen.getByText(title).closest("[data-tone]")?.textContent ?? "";
  expect(screen.getByText("Sign-in tokens written")).toBeTruthy();
  expect(tile("Sign-in tokens written")).toContain("2");
  expect(tile("Refreshes answered")).toContain("2");
  expect(tile("Blocked by policy")).toContain("1");
  expect(screen.getAllByText("of 5").length).toBeGreaterThanOrEqual(4);
});
