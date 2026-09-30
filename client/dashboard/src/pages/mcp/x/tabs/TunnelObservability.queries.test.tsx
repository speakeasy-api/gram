import { cleanup, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ErrorBoundary } from "react-error-boundary";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { isUnauthorizedError } from "@/lib/route-errors";
import { TunneledMcpConnectionsPanel } from "./TunneledMcpConnectionsPanel";

const requests = vi.hoisted(() => ({ history: vi.fn(), connections: vi.fn() }));
// Mock requests so the generated hooks and QueryClient handle each rejection.
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/getTunneledMcpServerMetrics.core.js", () => ({
  buildGetTunneledMcpServerMetricsQuery: () => ({
    queryKey: ["history"],
    queryFn: requests.history,
  }),
}));
vi.mock(
  "@gram/client/react-query/listTunneledMcpServerConnections.core.js",
  () => ({
    buildListTunneledMcpServerConnectionsQuery: () => ({
      queryKey: ["connections"],
      queryFn: requests.connections,
    }),
  }),
);
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({ data: { mcpServers: [] }, isError: false }),
}));
vi.mock("@gram/client/react-query/getTunneledMcpServer.js", () => ({
  useGetTunneledMcpServer: () => ({}),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => true }),
}));
vi.mock("react-chartjs-2", () => ({ Line: () => <div /> }));

beforeEach(() => {
  requests.history.mockResolvedValue({
    state: "available",
    points: [
      {
        time: new Date(),
        toolCalls: 7,
        successes: 7,
        errors: 0,
        coverageSamples: 1,
        requestCoverageSamples: 1,
        collectionPartial: false,
      },
    ],
    clients: [],
  });
  requests.connections.mockResolvedValue({
    activeConnectionCount: 1,
    activeConsumerSessionCount: 0,
    connections: [
      {
        activeConsumerSessions: 0,
        activeSubstreams: 0,
        agentVersion: "0.2.0",
        connectedAt: new Date(),
        gatewaySessionId: "synthetic-session",
        lastHeartbeatAt: new Date(),
        metadata: {},
        serviceVersion: "test",
        diagnostics: { state: "available", targetState: "reachable" },
      },
    ],
  });
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        // Same network/5xx escalation policy as the application QueryClient.
        throwOnError: (error) =>
          !isUnauthorizedError(error) &&
          !(error instanceof GramError && error.statusCode === 403),
      },
    },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ErrorBoundary fallback={<p>Overview crashed</p>}>
          <TunneledMcpConnectionsPanel
            tunneledMcpServerId="source"
            agentSetupHref="/settings#agent"
          />
        </ErrorBoundary>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}

it("keeps live diagnostics usable when the history request rejects", async () => {
  requests.history.mockRejectedValue(new TypeError("Failed to fetch"));
  const client = renderPanel();
  expect(
    await screen.findByText("Activity history is unavailable"),
  ).toBeTruthy();
  expect(await screen.findByText("Network reachable")).toBeTruthy();
  expect(screen.queryByText("Overview crashed")).toBeNull();
  client.clear();
});

it("keeps activity history usable when the live request rejects", async () => {
  requests.connections.mockRejectedValue(new Error("503 Service unavailable"));
  const client = renderPanel();
  expect(await screen.findByText("Live status is unavailable")).toBeTruthy();
  expect(
    await within(
      screen.getByRole("group", { name: "MCP responses" }),
    ).findByText("7"),
  ).toBeTruthy();
  expect(screen.queryByText("Overview crashed")).toBeNull();
  client.clear();
});
