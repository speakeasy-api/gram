import type { ReadinessCheck } from "@/components/mcp-server-readiness-bar";
import { getTunneledMcpServerArgs } from "@/lib/sources";
import type { TunneledMcpConnection } from "@gram/client/models/components/tunneledmcpconnection.js";
import type { TunneledMcpServerConnections } from "@gram/client/models/components/tunneledmcpserverconnections.js";
import {
  type ListTunneledMcpServerConnectionsQueryData,
  type ListTunneledMcpServerConnectionsQueryError,
  useListTunneledMcpServerConnections,
} from "@gram/client/react-query/listTunneledMcpServerConnections.js";
import type { UseQueryResult } from "@tanstack/react-query";

// Live sessions come from Redis heartbeats. The status view polls on this
// cadence and shows its own error state when a poll fails.
const TUNNEL_STATUS_POLL_MS = 30_000;

// One query for a tunnel's live connections, shared by the Overview status and
// the sidebar readiness card. Only the Overview polls: refetchInterval runs per
// observer, so a second poller would double the requests. Other readers get
// the cached snapshot, refreshed on mount and window focus.
export function useTunneledMcpConnections(
  tunneledMcpServerId: string,
  { poll = false }: { poll?: boolean } = {},
): UseQueryResult<
  ListTunneledMcpServerConnectionsQueryData,
  ListTunneledMcpServerConnectionsQueryError
> {
  return useListTunneledMcpServerConnections(
    getTunneledMcpServerArgs(tunneledMcpServerId),
    undefined,
    {
      enabled: tunneledMcpServerId !== "",
      // Status views keep rendering when a request fails.
      throwOnError: false,
      ...(poll
        ? {
            refetchInterval: TUNNEL_STATUS_POLL_MS,
            refetchIntervalInBackground: false,
          }
        : {}),
    },
  );
}

// A failed poll must not leave cached agents looking live.
export function liveTunnelConnections(
  connections: TunneledMcpServerConnections | undefined,
  error: boolean,
): { live: TunneledMcpConnection[]; unavailable: boolean } {
  return {
    live: connections?.connections ?? [],
    unavailable: error || connections?.collectionState === "unavailable",
  };
}

export type Tone = "success" | "warning" | "destructive" | "neutral";

export type TunnelStatus = {
  kind:
    | "unavailable"
    | "no-agents"
    | "unreachable"
    | "partially-reachable"
    | "some-checks-failed"
    | "reachable"
    | "checks-incomplete"
    | "not-checked";
  tone: Tone;
  title: string;
  detail: string;
  action?: { label: string; href: string };
};

// One sentence answers "is the tunnel working?"; the agent rows below explain
// it. Only the reachability checks agents reported recently count, so a
// legacy agent without diagnostics never reads as healthy or broken.
export function tunnelStatus(
  live: TunneledMcpConnection[],
  unavailable: boolean,
  links: { agentSetupHref: string },
): TunnelStatus {
  if (unavailable) {
    return {
      kind: "unavailable",
      tone: "warning",
      title: "Live status is unavailable",
      detail:
        "The latest connection status could not be loaded. Retrying automatically.",
    };
  }
  if (!live.length) {
    return {
      kind: "no-agents",
      tone: "neutral",
      title: "No connected tunnel agents",
      detail:
        "Requests to this server fail until a tunnel agent connects from the network that hosts it.",
      action: { label: "View tunnel agent setup", href: links.agentSetupHref },
    };
  }
  const agents = agentCount(live);
  const checked = live.filter(
    (c) =>
      c.diagnostics?.state === "available" &&
      (c.diagnostics.targetState === "reachable" ||
        c.diagnostics.targetState === "unreachable"),
  );
  const failing = checked.filter(
    (c) => c.diagnostics?.targetState === "unreachable",
  ).length;
  // A failing check is fixed where the agent's target is configured. Tool Logs
  // cannot show these failures: it records calls, not network checks.
  const setup = {
    label: "Review tunnel agent setup",
    href: links.agentSetupHref,
  };
  if (failing && failing === live.length) {
    return {
      kind: "unreachable",
      tone: "destructive",
      title: "Unreachable",
      detail: `${agents} connected, but none can reach the MCP server.`,
      action: setup,
    };
  }
  if (failing && failing < checked.length) {
    return {
      kind: "partially-reachable",
      tone: "warning",
      title: "Partially reachable",
      detail: `${failing} of ${agents} cannot reach the MCP server.`,
      action: setup,
    };
  }
  if (failing) {
    return {
      kind: "some-checks-failed",
      tone: "warning",
      title: "Some checks failed",
      detail: `${failing} of ${agents} cannot reach the MCP server; the rest have not reported a check.`,
      action: setup,
    };
  }
  if (checked.length === live.length) {
    return {
      kind: "reachable",
      tone: "success",
      title: "Reachable",
      detail: `${agents} connected and can reach the MCP server's address on the network. Activity below shows whether MCP requests succeed.`,
    };
  }
  const checksOff = live.every((c) => c.diagnostics?.state === "disabled");
  return {
    kind: checked.length ? "checks-incomplete" : "not-checked",
    tone: "neutral",
    title: checked.length ? "Checks incomplete" : "Not checked",
    detail: checksOff
      ? `${agents} connected. Checks are turned off, so reachability is not reported.`
      : `${agents} connected. Reachability is reported once each tunnel agent runs its network checks.`,
  };
}

function agentCount(live: TunneledMcpConnection[]): string {
  return `${live.length} ${live.length === 1 ? "tunnel agent" : "tunnel agents"}`;
}

// The sidebar readiness item for a tunneled server, judged on the same status
// the Overview shows so the two never disagree. A missing agent or a failing
// check is a step to take; anything not yet known is neither done nor failed.
export function tunnelAgentReadiness({
  connections,
  loading,
  error,
  agentSetupHref,
  overviewHref,
}: {
  connections: TunneledMcpServerConnections | undefined;
  loading: boolean;
  error: boolean;
  agentSetupHref: string;
  overviewHref: string;
}): ReadinessCheck {
  const item = { key: "tunnel-agent", label: "Tunnel agent" };
  if (loading) {
    return {
      ...item,
      description: "Checking for connected tunnel agents.",
      ready: null,
      href: overviewHref,
    };
  }
  const { live, unavailable } = liveTunnelConnections(connections, error);
  const status = tunnelStatus(live, unavailable, { agentSetupHref });
  switch (status.kind) {
    case "unavailable":
      return {
        ...item,
        description: "Tunnel status could not be loaded.",
        ready: null,
        href: overviewHref,
      };
    case "no-agents":
      return {
        ...item,
        description:
          "No tunnel agent is connected, so requests to this server fail. View tunnel agent setup to connect one.",
        ready: false,
        href: agentSetupHref,
      };
    case "unreachable":
    case "partially-reachable":
    case "some-checks-failed":
      return {
        ...item,
        description: `${status.detail} Review tunnel agent setup.`,
        ready: false,
        href: agentSetupHref,
      };
    case "reachable":
      return {
        ...item,
        description: `${agentCount(live)} connected and can reach the MCP server.`,
        ready: true,
        href: overviewHref,
      };
    case "checks-incomplete":
    case "not-checked":
      // An agent is connected, but nothing says it can reach the server.
      return {
        ...item,
        description: status.detail,
        ready: null,
        href: overviewHref,
      };
  }
}
