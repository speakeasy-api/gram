import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
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
          logsHref="/logs?af=gram.tunneled_mcp_server.id%3Aeq%3Asource"
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
          logsHref="/logs?af=gram.tunneled_mcp_server.id%3Aeq%3Asource"
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
          logsHref="/logs?af=gram.tunneled_mcp_server.id%3Aeq%3Asource"
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
        logsHref="/logs?af=gram.tunneled_mcp_server.id%3Aeq%3Asource"
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
      lastHttpStatus: 0,
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
        logsHref="/logs?af=gram.tunneled_mcp_server.id%3Aeq%3Asource"
        agentSetupHref="/settings#agent"
      />
    </MemoryRouter>,
  );
  expect(screen.getByText(text)).toBeTruthy();
  if (_name === "idle") {
    const details = screen.getByText("Connection details").closest("details")!;
    expect(within(details).queryByText("0", { exact: true })).toBeNull();
    expect(within(details).queryByText(/Last HTTP response/)).toBeNull();
  }
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
        logsHref="/logs?af=gram.tunneled_mcp_server.id%3Aeq%3Asource"
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
  ["unreachable", "Unreachable"],
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
        logsHref="/logs?af=gram.tunneled_mcp_server.id%3Aeq%3Asource"
        agentSetupHref="/settings"
      />
    </MemoryRouter>,
  );
  expect(
    within(screen.getByRole("group", { name: "Transport status" })).getByText(
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
        logsHref="/logs?af=gram.tunneled_mcp_server.id%3Aeq%3Asource"
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
        logsHref="/logs?af=gram.tunneled_mcp_server.id%3Aeq%3Asource"
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
        logsHref="/logs?af=gram.tunneled_mcp_server.id%3Aeq%3Asource"
        agentSetupHref="/settings"
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("Live status is unavailable")).toBeTruthy();
  expect(screen.queryByText("No connected agents")).toBeNull();
});

it("keeps failure guidance visible and reveals dependent checks and HTTP details on demand", () => {
  render(
    <MemoryRouter>
      <TunnelObservability
        id="source"
        loading={false}
        error={false}
        agentSetupHref="/settings"
        logsHref="/logs?af=tunnel-filter"
        connections={{
          ...legacy,
          connections: [
            {
              ...legacy.connections[0]!,
              diagnostics: {
                state: "available",
                targetState: "unreachable",
                requestsTotal: 5,
                dns: { state: "fail", failure: "dns_not_found", durationMs: 1 },
                tcp: { state: "not_tested", failure: "", durationMs: 0 },
                tls: { state: "not_applicable", failure: "", durationMs: 0 },
                httpProgress: { waitingHeaders: 2, openResponses: 0 },
                lastHttpStatus: 200,
                lastHttpResponseAgeMs: 7_866_000,
              },
            },
          ],
        }}
      />
    </MemoryRouter>,
  );
  expect(
    screen.getByText(/Hostname was not found/).closest("details"),
  ).toBeNull();
  expect(
    screen.getByRole("link", { name: "View tool logs" }).getAttribute("href"),
  ).toBe("/logs?af=tunnel-filter");
  const details = screen.getByText("Connection details").closest("details")!;
  expect(details.open).toBe(false);
  expect(
    screen.getByText("Waiting for response headers").closest("details"),
  ).toBe(details);
  expect(screen.getByText(/Last HTTP response: 200/).closest("details")).toBe(
    details,
  );
  expect(screen.queryByText(/7866s ago/)).toBeNull();
  fireEvent.click(screen.getByText("Connection details"));
  expect(details.open).toBe(true);
  expect(screen.getByText("Not checked (DNS failed)")).toBeTruthy();
  expect(screen.queryByText("Linked MCP servers")).toBeNull();
});

it("distinguishes agents that disagree about target reachability", () => {
  render(
    <MemoryRouter>
      <TunnelObservability
        id="source"
        loading={false}
        error={false}
        agentSetupHref="/settings"
        logsHref="/logs"
        connections={{
          ...legacy,
          activeConnectionCount: 2,
          connections: [
            {
              ...legacy.connections[0]!,
              diagnostics: { state: "available", targetState: "reachable" },
            },
            {
              ...legacy.connections[0]!,
              gatewaySessionId: "another-session",
              diagnostics: { state: "available", targetState: "unreachable" },
            },
          ],
        }}
      />
    </MemoryRouter>,
  );
  expect(
    within(screen.getByRole("group", { name: "Transport status" })).getByText(
      "Partially reachable",
    ),
  ).toBeTruthy();
  expect(screen.getByText("Network reachable")).toBeTruthy();
  expect(screen.getByText("Target unreachable")).toBeTruthy();
});

it.each([
  undefined,
  { state: "stale", targetState: "reachable" },
  { state: "available", targetState: "unknown" },
] as const)(
  "does not treat an agent with unknown checks as unreachable (%j)",
  (diagnostics) => {
    render(
      <MemoryRouter>
        <TunnelObservability
          id="source"
          loading={false}
          error={false}
          agentSetupHref="/settings"
          logsHref="/logs"
          connections={{
            ...legacy,
            activeConnectionCount: 2,
            connections: [
              {
                ...legacy.connections[0]!,
                diagnostics: { state: "available", targetState: "unreachable" },
              },
              {
                ...legacy.connections[0]!,
                gatewaySessionId: "another-session",
                diagnostics,
              },
            ],
          }}
        />
      </MemoryRouter>,
    );
    const status = screen.getByRole("group", { name: "Transport status" });
    expect(within(status).getByText("Some checks failed")).toBeTruthy();
    expect(
      within(status).queryByText("Unreachable", { exact: true }),
    ).toBeNull();
    expect(
      within(status).getByRole("link", { name: "View tool logs" }),
    ).toBeTruthy();
  },
);
