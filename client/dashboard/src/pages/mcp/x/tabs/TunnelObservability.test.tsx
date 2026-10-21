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

type ChartData = {
  datasets: { label: string; pointRadius: number[] }[];
};
const charts = vi.hoisted(() => new Map<string, ChartData>());
vi.mock("react-chartjs-2", () => ({
  Line: (props: { "aria-label": string; data: ChartData }) => {
    charts.set(props["aria-label"], props.data);
    return <div role="img" aria-label={props["aria-label"]} />;
  },
}));

const mocks = vi.hoisted(() => ({ history: vi.fn() }));
vi.mock("@gram/client/react-query/getTunneledMcpServerMetrics.js", () => ({
  useGetTunneledMcpServerMetrics: (...args: unknown[]) =>
    mocks.history(...args),
}));
beforeEach(() =>
  mocks.history.mockReturnValue({
    data: { state: "unavailable", points: [], clients: [] },
    isPending: false,
    isError: false,
  }),
);

afterEach(() => {
  cleanup();
  charts.clear();
});

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
    expect(screen.getByText(/Update the agent and\s+gateway/)).toBeTruthy();
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
    const setup = screen.getByRole("link", { name: "View agent setup" });
    expect(setup.getAttribute("href")).toBe("/settings#agent");
    // One control: the link is styled as a button, not wrapped around one.
    expect(setup.querySelector("button")).toBeNull();
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
  expect(screen.queryByRole("group", { name: "MCP responses" })).toBeNull();
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
    "disabled",
    { state: "disabled" },
    "Checks are turned off for this agent, so its reachability is not reported.",
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
    within(screen.getByRole("group", { name: "Tunnel status" })).getByText(
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
  expect(screen.getByText("Worst interval's p95 response time")).toBeTruthy();
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
  expect(screen.getByRole("group", { name: "Tunnel status" })).toBeTruthy();
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
  // A failing check is fixed where the agent is configured. Tool Logs records
  // calls, not network checks, so it stays a separate link in Activity.
  const setup = screen.getByRole("link", { name: "Review agent setup" });
  expect(setup.getAttribute("href")).toBe("/settings");
  expect(setup.querySelector("button")).toBeNull();
  expect(screen.queryByRole("link", { name: "View tool logs" })).toBeNull();
  const calls = screen.getByRole("link", { name: "View tool calls" });
  expect(calls.getAttribute("href")).toBe("/logs?af=tunnel-filter");
  const details = screen.getByText("Connection details").closest("details")!;
  expect(details.open).toBe(false);
  expect(
    screen.getByText("Waiting for response headers").closest("details"),
  ).toBe(details);
  expect(screen.getByText(/Last HTTP response: 200/).closest("details")).toBe(
    details,
  );
  expect(
    screen.getByText(/Last HTTP response: 200 · about 2 hours ago/),
  ).toBeTruthy();
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
    within(screen.getByRole("group", { name: "Tunnel status" })).getByText(
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
    const status = screen.getByRole("group", { name: "Tunnel status" });
    expect(within(status).getByText("Some checks failed")).toBeTruthy();
    expect(
      within(status).queryByText("Unreachable", { exact: true }),
    ).toBeNull();
    expect(
      within(status).getByRole("link", { name: "Review agent setup" }),
    ).toBeTruthy();
  },
);

it("shows activity from other MCP methods", () => {
  mocks.history.mockReturnValue({
    data: {
      state: "available",
      points: [{ time: new Date(), otherRequests: 7 }],
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
        logsHref="/logs"
        agentSetupHref="/settings"
      />
    </MemoryRouter>,
  );
  expect(screen.getByRole("img", { name: "MCP requests" })).toBeTruthy();
  expect(screen.getByText("Other requests")).toBeTruthy();
  expect(
    within(screen.getByRole("group", { name: "Requests" })).getByText("7"),
  ).toBeTruthy();
  expect(
    within(screen.getByRole("group", { name: "MCP responses" })).getByText("0"),
  ).toBeTruthy();
});

function renderWith(
  connections: TunneledMcpServerConnections["connections"],
): void {
  render(
    <MemoryRouter>
      <TunnelObservability
        id="source"
        connections={{ ...legacy, connections }}
        loading={false}
        error={false}
        logsHref="/logs"
        agentSetupHref="/settings"
      />
    </MemoryRouter>,
  );
}

it("says checks are off for a disabled agent without suggesting an update", () => {
  renderWith([
    {
      ...legacy.connections[0]!,
      diagnostics: { state: "disabled", receivedAt: new Date() },
    },
  ]);
  expect(screen.getByText("Checks turned off")).toBeTruthy();
  expect(
    screen.getByText(
      "Checks are turned off for this agent, so its reachability is not reported.",
    ),
  ).toBeTruthy();
  expect(
    screen.getByText(
      "1 agent connected. Checks are turned off, so reachability is not reported.",
    ),
  ).toBeTruthy();
  expect(screen.queryByText(/Update the agent/)).toBeNull();
  expect(screen.queryByText("Connection details")).toBeNull();
  expect(screen.queryByText(/may no longer reflect the target/)).toBeNull();
});

it("describes Reachable as network reachability, not MCP health", () => {
  renderWith([
    {
      ...legacy.connections[0]!,
      diagnostics: { state: "available", targetState: "reachable" },
    },
  ]);
  expect(
    within(screen.getByRole("group", { name: "Tunnel status" })).getByText(
      /can reach the MCP server's address on the network/,
    ),
  ).toBeTruthy();
});

it("explains a server error response inside the connection details", () => {
  renderWith([
    {
      ...legacy.connections[0]!,
      diagnostics: {
        state: "available",
        targetState: "reachable",
        requestsTotal: 3,
        lastHttpStatus: 503,
      },
    },
  ]);
  const details = screen.getByText("Connection details").closest("details")!;
  expect(
    within(details).getByText(
      /Last HTTP response: 503\. The MCP server answered with a server error\. Check the server's logs\./,
    ),
  ).toBeTruthy();
  expect(within(details).queryByText(/credentials may be needed/)).toBeNull();
});

it("labels every client family the collector reports", () => {
  const families = [
    "claude",
    "codex",
    "cursor",
    "chatgpt",
    "inspector",
    "unknown",
    "other",
  ];
  mocks.history.mockReturnValue({
    data: {
      state: "available",
      points: [{ time: new Date(), toolCalls: 7 }],
      clients: families.map((family, i) => ({ family, requests: i + 1 })),
    },
    isPending: false,
    isError: false,
  });
  renderWith(legacy.connections);
  for (const label of [
    "Claude",
    "Codex",
    "Cursor",
    "ChatGPT",
    "MCP Inspector",
    "Unknown client",
    "Other clients",
  ]) {
    expect(screen.getByText(label)).toBeTruthy();
  }
});

it("explains which requests the numbers include", () => {
  mocks.history.mockReturnValue({
    data: {
      state: "available",
      points: [{ time: new Date(), toolCalls: 1 }],
      clients: [],
    },
    isPending: false,
    isError: false,
  });
  renderWith(legacy.connections);
  const notes = screen.getByText("About these numbers").closest("details")!;
  expect(
    within(notes).getByText(
      /identified by the name it reports when it connects, falling back to the HTTP User-Agent/,
    ),
  ).toBeTruthy();
  expect(
    within(notes).getByText(/Requests that Speakeasy sends on a user/),
  ).toBeTruthy();
  expect(
    within(notes).getByText(
      "Checks that Speakeasy runs to validate a connection are not counted.",
    ),
  ).toBeTruthy();
});

it("refreshes longer history windows less often", () => {
  renderWith(legacy.connections);
  const interval = () => mocks.history.mock.calls.at(-1)?.[2]?.refetchInterval;
  expect(interval()).toBe(5 * 60_000);
  fireEvent.click(screen.getByRole("button", { name: "1 hour" }));
  expect(interval()).toBe(30_000);
  fireEvent.click(screen.getByRole("button", { name: "7 days" }));
  expect(interval()).toBe(15 * 60_000);
});

it("marks values with no neighbour so isolated samples stay visible", () => {
  const start = Date.UTC(2026, 0, 1);
  const at = (minute: number) => new Date(start + minute * 60_000);
  mocks.history.mockReturnValue({
    data: {
      state: "available",
      points: [
        { time: at(0), toolCalls: 1 },
        { time: at(1), toolCalls: 2 },
        { time: at(2) },
        { time: at(3), toolCalls: 4 },
        { time: at(4) },
      ],
      clients: [],
    },
    isPending: false,
    isError: false,
  });
  renderWith(legacy.connections);
  const datasets = charts.get("MCP requests")!.datasets;
  const toolCalls = datasets.find((d) => d.label === "Tool calls")!;
  expect(toolCalls.pointRadius).toEqual([0, 0, 0, 3, 0]);
  const errors = datasets.find((d) => d.label === "Errors")!;
  expect(errors.pointRadius).toEqual([0, 0, 0, 0, 0]);
});

it("shows requests blocked by Speakeasy apart from errors", () => {
  mocks.history.mockReturnValue({
    data: {
      state: "available",
      points: [
        {
          time: new Date(),
          toolCalls: 10,
          successes: 6,
          errors: 2,
          rejected: 2,
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
        logsHref="/logs"
        agentSetupHref="/settings"
      />
    </MemoryRouter>,
  );
  const errors = within(screen.getByRole("group", { name: "Errors" }));
  expect(errors.getByText("2")).toBeTruthy();
  // Blocked requests never reached the MCP server, so they stay out of the error rate.
  expect(
    errors.getByText("25% of completed requests · 2 blocked by Speakeasy"),
  ).toBeTruthy();
});
