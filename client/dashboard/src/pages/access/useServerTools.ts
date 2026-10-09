import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useToolMetadata } from "@/hooks/useToolMetadata";
import { platformEndpointSlug } from "@/hooks/useToolsetUrl";
import { getServerURL } from "@/lib/utils";
import { useGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMemo } from "react";

import { useRemoteMcpToolConnection } from "@/pages/mcp/x/tabs/useRemoteMcpToolConnection";
import { useTunnelAgentStatus } from "@/pages/mcp/x/tabs/useTunnelAgentStatus";
import type { ServerWithProject } from "./mcpAccessModel";
import {
  proxiedToolsToServerTools,
  toolMetadataToServerTools,
} from "./remoteToolMetadata";
import type { Server, ServerTool } from "./serverMerge";

/**
 * Where a server's tool list comes from. Toolset servers know their tools at
 * deploy time. Remote and tunneled servers know them once they are stored, or
 * once a live session lists them; with no upstream session yet they need
 * connecting, and a tunnel with no agent connected cannot list them at all.
 * Unproxied servers carry no Speakeasy traffic, so their tools are never known.
 */
export type ToolSource =
  | { status: "ready"; tools: ServerTool[] }
  | { status: "loading" }
  | { status: "error"; retry: () => void }
  /** `connect` is undefined when the server has no connect page. */
  | { status: "needs-connect"; connect: (() => void) | undefined }
  /**
   * Nothing is stored and this editor cannot store it: recording a server's
   * tools takes `mcp:write` on it, so no live session is opened.
   */
  | { status: "needs-write" }
  /** Nothing is stored and the tunnel has no agent connected to list from. */
  | { status: "offline"; retry: () => void }
  | { status: "dynamic" }
  | { status: "none" };

/** The tools of one server in the role editor's tool access sheet. */
export function useServerTools(
  entry: ServerWithProject | undefined,
): ToolSource {
  const organization = useOrganization();
  const server = entry?.server;
  const proxied = !!server?.dynamicTools && server.storedToolInventory;
  const project = organization.projects.find((p) => p.id === entry?.projectId);
  const projectRef = useMemo(
    () => (project ? { id: project.id, slug: project.slug } : undefined),
    [project],
  );

  // Named by project: on this org-level page an unnamed request would resolve
  // against whatever project the URL happens to carry.
  const stored = useToolMetadata(server?.id, {
    enabled: proxied && !!project,
    projectSlug: project?.slug,
  });
  const storedTools = useMemo(
    () =>
      server
        ? toolMetadataToServerTools(
            server.id,
            Object.values(stored.metadataByTool),
          )
        : [],
    [server, stored.metadataByTool],
  );

  // Recording a server's tools takes mcp:write on it (useSyncToolMetadata), so
  // an editor without it gets no live session: a list nobody can store would
  // only be a preview of tools the role cannot be limited to reliably.
  const { hasAnyScope } = useRBAC();
  const canRecordTools =
    !!server && hasAnyScope(["mcp:write"], server.id, project?.id);

  // Only a proxied server with nothing stored opens a live session, the way
  // the Inspect tab does; one that lists records its tools as it goes.
  // A failed metadata read is no reason to give up: the server may still list
  // its tools live.
  const nothingStored =
    proxied && !!project && !stored.isLoading && storedTools.length === 0;
  const needsLive = nothingStored && canRecordTools;
  const mcpServer = useGetMcpServer(
    { id: server?.id, gramProject: project?.slug },
    undefined,
    { enabled: needsLive && !!project, throwOnError: false },
  );
  const endpoints = useMcpEndpoints(
    { mcpServerId: server?.id, gramProject: project?.slug },
    undefined,
    { enabled: needsLive && !!project, throwOnError: false },
  );
  // Connect on the Speakeasy origin: a custom domain would need its own session.
  const platformSlug = platformEndpointSlug(endpoints.data?.mcpEndpoints ?? []);
  const live = useRemoteMcpToolConnection({
    mcpUrl: platformSlug ? `${getServerURL()}/mcp/${platformSlug}` : undefined,
    mcpServerId: server?.id,
    userSessionIssuerId: mcpServer.data?.userSessionIssuerId,
    remoteMcpServerId: mcpServer.data?.remoteMcpServerId ?? undefined,
    platformSlug,
    tunneledMcpServerId: mcpServer.data?.tunneledMcpServerId,
    visibility: mcpServer.data?.visibility,
    project: projectRef,
    enabled: needsLive && !!mcpServer.data && !!platformSlug,
  });
  // A cached listing can surface while the session token is still minting;
  // only a settled, successful listing counts.
  const liveListed = live.listed && !live.loading;
  const tunnel = useTunnelAgentStatus({
    tunneledSourceId: server?.tunneledSourceId,
    projectSlug: project?.slug,
    enabled: needsLive,
    poll: needsLive && !liveListed,
    onReconnect: live.refetch,
  });
  const liveTools = useMemo(
    () =>
      server && live.tools
        ? proxiedToolsToServerTools(server.id, live.tools)
        : undefined,
    [server, live.tools],
  );

  return resolveToolSource({
    server,
    stored: {
      isLoading: stored.isLoading,
      isError: stored.isError,
      tools: storedTools,
      retry: stored.refetch,
    },
    canRecordTools,
    target: {
      isLoading: mcpServer.isLoading || endpoints.isLoading,
      isError: mcpServer.isError || endpoints.isError,
      retry: () => {
        void mcpServer.refetch();
        void endpoints.refetch();
      },
    },
    platformSlug,
    live: {
      loading: live.loading,
      needsAuth: live.needsAuth,
      isError: live.isError,
      tools: liveTools,
      connect: live.connect,
      retry: live.refetch,
    },
    tunnel: {
      offline: tunnel.offline,
      retry: () => {
        tunnel.refetch();
        live.refetch();
      },
    },
  });
}

/** What {@link resolveToolSource} reads from each query behind a server. */
export interface ToolSourceInputs {
  server: Server | undefined;
  stored: {
    isLoading: boolean;
    isError: boolean;
    tools: ServerTool[];
    retry: () => void;
  };
  /** The editor holds mcp:write on the server, so may record its tools. */
  canRecordTools: boolean;
  /** The server record and endpoints a live session is opened through. */
  target: { isLoading: boolean; isError: boolean; retry: () => void };
  platformSlug: string | undefined;
  live: {
    loading: boolean;
    needsAuth: boolean;
    isError: boolean;
    /**
     * The latest listing, which may be cached from an earlier session. Only
     * used once the session has settled without an error.
     */
    tools: ServerTool[] | undefined;
    connect: (() => void) | undefined;
    retry: () => void;
  };
  tunnel: { offline: boolean; retry: () => void };
}

/**
 * Picks where a server's tools come from. Stored tools win; a live listing
 * that succeeded wins over a tunnel status saying the agent is offline; that
 * status only explains a listing that could not happen.
 */
export function resolveToolSource({
  server,
  stored,
  canRecordTools,
  target,
  platformSlug,
  live,
  tunnel,
}: ToolSourceInputs): ToolSource {
  if (!server) return { status: "none" };
  if (!server.dynamicTools) return { status: "ready", tools: server.tools };
  if (!server.storedToolInventory) return { status: "dynamic" };
  if (stored.isLoading) return { status: "loading" };
  if (stored.tools.length > 0) return { status: "ready", tools: stored.tools };
  if (!canRecordTools) {
    return stored.isError
      ? { status: "error", retry: stored.retry }
      : { status: "needs-write" };
  }
  if (target.isError) return { status: "error", retry: target.retry };
  if (target.isLoading) return { status: "loading" };
  if (live.tools && !live.loading && !live.isError) {
    return { status: "ready", tools: live.tools };
  }
  if (tunnel.offline) return { status: "offline", retry: tunnel.retry };
  // No Speakeasy-origin endpoint: nothing to list through or connect to.
  if (!platformSlug) return { status: "needs-connect", connect: undefined };
  if (live.loading) return { status: "loading" };
  if (live.needsAuth) return { status: "needs-connect", connect: live.connect };
  if (live.isError) return { status: "error", retry: live.retry };
  return { status: "ready", tools: [] };
}
