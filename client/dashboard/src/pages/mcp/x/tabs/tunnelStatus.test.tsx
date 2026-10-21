import { McpServerReadinessBar } from "@/components/mcp-server-readiness-bar";
import type { TunnelDiagnostics } from "@gram/client/models/components/tunneldiagnostics.js";
import type { TunneledMcpConnection } from "@gram/client/models/components/tunneledmcpconnection.js";
import type { TunneledMcpServerConnections } from "@gram/client/models/components/tunneledmcpserverconnections.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import {
  liveTunnelConnections,
  tunnelAgentReadiness,
  tunnelStatus,
} from "./tunnelStatus";

afterEach(cleanup);

const links = { agentSetupHref: "/settings#agent-setup", overviewHref: "/" };

function agent(
  id: string,
  diagnostics?: TunnelDiagnostics,
): TunneledMcpConnection {
  return {
    activeConsumerSessions: 0,
    activeSubstreams: 0,
    agentVersion: "0.2.0",
    connectedAt: new Date(),
    gatewaySessionId: id,
    lastHeartbeatAt: new Date(),
    metadata: {},
    serviceVersion: "test",
    diagnostics,
  };
}

const reachable: TunnelDiagnostics = {
  state: "available",
  targetState: "reachable",
};
const unreachable: TunnelDiagnostics = {
  state: "available",
  targetState: "unreachable",
};

function snapshot(
  connections: TunneledMcpConnection[],
  collectionState?: string,
): TunneledMcpServerConnections {
  return {
    activeConnectionCount: connections.length,
    activeConsumerSessionCount: 0,
    connections,
    collectionState,
  };
}

// Each case is one tunnel state: the Overview title it shows, and the sidebar
// readiness item beside it.
const cases: {
  name: string;
  connections?: TunneledMcpServerConnections;
  error?: boolean;
  overview: string;
  ready: boolean | null;
  description: string;
  href: string;
}[] = [
  {
    name: "a failed poll",
    connections: snapshot([agent("a", reachable)]),
    error: true,
    overview: "Live status is unavailable",
    ready: null,
    description: "Tunnel status could not be loaded.",
    href: links.overviewHref,
  },
  {
    name: "an unavailable collection",
    connections: snapshot([], "unavailable"),
    overview: "Live status is unavailable",
    ready: null,
    description: "Tunnel status could not be loaded.",
    href: links.overviewHref,
  },
  {
    name: "no connected agents",
    connections: snapshot([]),
    overview: "No connected agents",
    ready: false,
    description:
      "No agent is connected, so requests to this server fail. View agent setup to connect one.",
    href: links.agentSetupHref,
  },
  {
    name: "every agent failing its check",
    connections: snapshot([agent("a", unreachable)]),
    overview: "Unreachable",
    ready: false,
    description:
      "1 agent connected, but none can reach the MCP server. Review agent setup.",
    href: links.agentSetupHref,
  },
  {
    name: "some agents failing their check",
    connections: snapshot([agent("a", unreachable), agent("b", reachable)]),
    overview: "Partially reachable",
    ready: false,
    description:
      "1 of 2 agents cannot reach the MCP server. Review agent setup.",
    href: links.agentSetupHref,
  },
  {
    name: "a failing check beside an unchecked agent",
    connections: snapshot([agent("a", unreachable), agent("b")]),
    overview: "Some checks failed",
    ready: false,
    description:
      "1 of 2 agents cannot reach the MCP server; the rest have not reported a check. Review agent setup.",
    href: links.agentSetupHref,
  },
  {
    name: "every agent reaching the server",
    connections: snapshot([agent("a", reachable), agent("b", reachable)]),
    overview: "Reachable",
    ready: true,
    description: "2 agents connected and can reach the MCP server.",
    href: links.overviewHref,
  },
  {
    name: "an agent that does not report checks",
    connections: snapshot([agent("a")]),
    overview: "Not checked",
    ready: null,
    description:
      "1 agent connected. Reachability is reported once each agent runs its network checks.",
    href: links.overviewHref,
  },
  {
    name: "checks turned off",
    connections: snapshot([agent("a", { state: "disabled" })]),
    overview: "Not checked",
    ready: null,
    description:
      "1 agent connected. Checks are turned off, so reachability is not reported.",
    href: links.overviewHref,
  },
  {
    name: "a passing check beside an unchecked agent",
    connections: snapshot([
      agent("a", reachable),
      agent("b", { state: "pending" }),
    ]),
    overview: "Checks incomplete",
    ready: null,
    description:
      "2 agents connected. Reachability is reported once each agent runs its network checks.",
    href: links.overviewHref,
  },
];

describe("tunnel agent readiness", () => {
  it.each(cases)(
    "matches the Overview for $name",
    ({ connections, error = false, overview, ready, description, href }) => {
      const { live, unavailable } = liveTunnelConnections(connections, error);
      expect(tunnelStatus(live, unavailable, links).title).toBe(overview);
      expect(
        tunnelAgentReadiness({ connections, loading: false, error, ...links }),
      ).toEqual({
        key: "tunnel-agent",
        label: "Tunnel agent",
        ready,
        description,
        href,
      });
    },
  );

  it("is undetermined while the first snapshot loads", () => {
    expect(
      tunnelAgentReadiness({
        connections: undefined,
        loading: true,
        error: false,
        ...links,
      }),
    ).toMatchObject({
      ready: null,
      description: "Checking for connected agents.",
    });
  });
});

describe("readiness summary", () => {
  const configured = {
    key: "server-url",
    label: "Server URL",
    description: "Endpoint is live and ready to connect to.",
    ready: true,
  };

  it.each([
    [true, "This MCP server is ready to be used."],
    [false, "This MCP server needs 1 more step before it's ready."],
    [null, "This MCP server's readiness can't be confirmed."],
  ] as const)("with a tunnel agent check of %s says %s", (ready, text) => {
    render(
      <MemoryRouter>
        <McpServerReadinessBar
          checks={[configured, { ...configured, key: "tunnel-agent", ready }]}
        />
      </MemoryRouter>,
    );
    expect(screen.getByText(text)).toBeTruthy();
  });
});
