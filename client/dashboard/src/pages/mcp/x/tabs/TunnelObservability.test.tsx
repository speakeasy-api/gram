import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TunneledMcpServerConnections } from "@gram/client/models/components/tunneledmcpserverconnections.js";
import { TunnelObservability } from "./TunnelObservability";

vi.mock("react-chartjs-2", () => ({
  Line: (props: { "aria-label": string }) => (
    <div role="img" aria-label={props["aria-label"]} />
  ),
}));

const mocks = vi.hoisted(() => ({ history: vi.fn() }));
vi.mock("@gram/client/react-query/getTunneledMcpServerMetrics.js", () => ({
  useGetTunneledMcpServerMetrics: () => mocks.history(),
}));
beforeEach(() =>
  mocks.history.mockReturnValue({
    data: { state: "unavailable", points: [], clients: [] },
    isPending: false,
    isError: false,
  }),
);

afterEach(cleanup);

const legacy: TunneledMcpServerConnections = {
  activeConnectionCount: 1,
  activeConsumerSessionCount: 0,
  connections: [
    {
      activeConsumerSessions: 0,
      activeSubstreams: 0,
      agentVersion: "0.1.0",
      connectedAt: new Date(),
      gatewaySessionId: "synthetic-session",
      lastHeartbeatAt: new Date(),
      metadata: {},
      serviceVersion: "test-service",
    },
  ],
};

describe("tunnel status evidence", () => {
  it("keeps legacy agents connected without claiming target reachability", () => {
    render(
      <MemoryRouter>
        <TunnelObservability
          id="source"
          connections={legacy}
          loading={false}
          error={false}
          agentSetupHref="/settings#agent"
        />
      </MemoryRouter>,
    );
    expect(screen.getByText("Diagnostics unsupported")).toBeTruthy();
    expect(screen.getByText("Not checked")).toBeTruthy();
    expect(screen.queryByText("Network reachable")).toBeNull();
    expect(screen.getByText("Activity history is unavailable")).toBeTruthy();
  });

  it("does not show a cached healthy agent as live after a failed poll", () => {
    const cached: TunneledMcpServerConnections = {
      ...legacy,
      connections: [
        {
          ...legacy.connections[0]!,
          diagnostics: { state: "available", targetState: "reachable" },
        },
      ],
    };
    render(
      <MemoryRouter>
        <TunnelObservability
          id="source"
          connections={cached}
          loading={false}
          error={true}
          agentSetupHref="/settings#agent"
        />
      </MemoryRouter>,
    );
    expect(screen.getByText("Live status is unavailable")).toBeTruthy();
    expect(screen.getByText("Unknown")).toBeTruthy();
    expect(screen.queryByText("Network reachable")).toBeNull();
    expect(screen.queryByText("No connected agents")).toBeNull();
  });

  it("provides setup navigation when the successful snapshot has no agents", () => {
    render(
      <MemoryRouter>
        <TunnelObservability
          id="source"
          connections={{ ...legacy, activeConnectionCount: 0, connections: [] }}
          loading={false}
          error={false}
          agentSetupHref="/settings#agent"
        />
      </MemoryRouter>,
    );
    expect(
      screen
        .getByRole("link", { name: "View agent setup" })
        .getAttribute("href"),
    ).toBe("/settings#agent");
    expect(screen.getByText("No connected agents")).toBeTruthy();
  });
});

it("does not color unavailable history green using cached successful responses", () => {
  mocks.history.mockReturnValue({
    data: {
      state: "available",
      points: [{ time: new Date(), toolsList: 3, successes: 3 }],
      clients: [],
    },
    isPending: false,
    isError: true,
  });
  render(
    <MemoryRouter>
      <TunnelObservability
        id="source"
        connections={legacy}
        loading={false}
        error={false}
        linkedServers={1}
        agentSetupHref="/settings#agent"
      />
    </MemoryRouter>,
  );
  const card = screen.getByRole("group", { name: "MCP responses" });
  expect(within(card).getByText("—")).toBeTruthy();
  expect(card.getAttribute("data-tone")).not.toBe("success");
  expect(screen.getByText("Activity history is unavailable")).toBeTruthy();
});

it.each([
  [
    "idle",
    {
      state: "available",
      requestsTotal: 0,
      httpProgress: { waitingHeaders: 0, openResponses: 0 },
    },
    "HTTP / MCP: Not observed. No traffic has reached this agent.",
  ],
  [
    "pending",
    { state: "pending" },
    "HTTP progress unavailable. Waiting for a fresh report.",
  ],
  [
    "legacy",
    { state: "available", requestsTotal: 2 },
    "HTTP progress unavailable for this agent.",
  ],
  [
    "stale",
    {
      state: "stale",
      requestsTotal: 2,
      httpProgress: { waitingHeaders: 1, openResponses: 1 },
    },
    "HTTP progress unavailable. Waiting for a fresh report.",
  ],
] as const)("represents %s evidence honestly", (_name, diagnostics, text) => {
  render(
    <MemoryRouter>
      <TunnelObservability
        id="source"
        connections={{
          ...legacy,
          connections: [{ ...legacy.connections[0]!, diagnostics }],
        }}
        loading={false}
        error={false}
        agentSetupHref="/settings#agent"
      />
    </MemoryRouter>,
  );
  expect(screen.getByText(text)).toBeTruthy();
  expect(screen.queryByText("Waiting for response headers")).toBeNull();
});

it("shows aggregate HTTP phases without calling an open stream a failure", () => {
  render(
    <MemoryRouter>
      <TunnelObservability
        id="source"
        connections={{
          ...legacy,
          connections: [
            {
              ...legacy.connections[0]!,
              diagnostics: {
                state: "available",
                targetState: "reachable",
                requestsTotal: 10000,
                httpProgress: { waitingHeaders: 12, openResponses: 24 },
                lastHttpStatus: 200,
              },
            },
          ],
        }}
        loading={false}
        error={false}
        agentSetupHref="/settings#agent"
      />
    </MemoryRouter>,
  );
  expect(
    screen.getByText("Waiting for response headers").nextElementSibling
      ?.textContent,
  ).toBe("12");
  expect(
    screen.getByText("Responses still open").nextElementSibling?.textContent,
  ).toBe("24");
  expect(screen.getByText(/Open streams may be expected/)).toBeTruthy();
  expect(screen.getByText("Network reachable")).toBeTruthy();
});

it.each([
  ["reachable", "Reachable"],
  ["unreachable", "1 unreachable"],
  ["unknown", "Not checked"],
] as const)("represents %s transport state", (targetState, summary) => {
  render(
    <MemoryRouter>
      <TunnelObservability
        id="source"
        connections={{
          ...legacy,
          connections: [
            {
              ...legacy.connections[0]!,
              diagnostics: { state: "available", targetState },
            },
          ],
        }}
        loading={false}
        error={false}
        agentSetupHref="/settings"
      />
    </MemoryRouter>,
  );
  expect(
    within(screen.getByRole("group", { name: "Target transport" })).getByText(
      summary,
    ),
  ).toBeTruthy();
});

it("renders completion-only history and its charts", () => {
  mocks.history.mockReturnValue({
    data: {
      state: "available",
      activeServers: 1,
      bucketSeconds: 60,
      points: [
        {
          time: new Date(),
          successes: 2,
          errors: 1,
          coverageSamples: 1,
          requestCoverageSamples: 1,
          collectionPartial: false,
        },
      ],
      clients: [],
    },
    isPending: false,
    isError: false,
  });
  render(
    <MemoryRouter>
      <TunnelObservability
        id="source"
        connections={legacy}
        loading={false}
        error={false}
        agentSetupHref="/settings"
      />
    </MemoryRouter>,
  );
  expect(
    within(screen.getByRole("group", { name: "MCP responses" })).getByText("2"),
  ).toBeTruthy();
  expect(screen.getByText("MCP requests")).toBeTruthy();
});

it("keeps live diagnostics visible when the history range is too large", () => {
  mocks.history.mockReturnValue({
    data: { state: "too_large", points: [], clients: [] },
    isPending: false,
    isError: false,
  });
  render(
    <MemoryRouter>
      <TunnelObservability
        id="source"
        connections={legacy}
        loading={false}
        error={false}
        agentSetupHref="/settings"
      />
    </MemoryRouter>,
  );
  expect(
    screen.getByText("This time range contains too much activity"),
  ).toBeTruthy();
  expect(screen.getByText("Agents & target checks")).toBeTruthy();
});

it("does not claim disconnection when collection is unavailable", () => {
  render(
    <MemoryRouter>
      <TunnelObservability
        id="source"
        connections={{ ...legacy, collectionState: "unavailable" }}
        loading={false}
        error={false}
        agentSetupHref="/settings"
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("Live status is unavailable")).toBeTruthy();
  expect(screen.queryByText("No connected agents")).toBeNull();
});
