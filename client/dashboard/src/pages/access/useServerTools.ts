import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useToolMetadata } from "@/hooks/useToolMetadata";
import { platformEndpointSlug } from "@/hooks/useToolsetUrl";
import { getServerURL } from "@/lib/utils";
import { useGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMemo } from "react";

import { useRemoteMcpToolConnection } from "@/pages/mcp/x/tabs/useRemoteMcpToolConnection";
import type { ServerWithProject } from "./mcpAccessModel";
import {
  proxiedToolsToServerTools,
  toolMetadataToServerTools,
} from "./remoteToolMetadata";
import type { ServerTool } from "./serverMerge";

/**
 * Where a server's tool list comes from. Toolset servers know their tools at
 * deploy time. Remote servers know them once they are stored, or once a live
 * session lists them; with no upstream session yet they need connecting.
 * Tunneled servers resolve their tools only when called.
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
  | { status: "dynamic" }
  | { status: "none" };

/** The tools of one server in the role editor's tool access sheet. */
export function useServerTools(
  entry: ServerWithProject | undefined,
): ToolSource {
  const organization = useOrganization();
  const server = entry?.server;
  const remote = !!server?.dynamicTools && server.remoteBacked;
  const project = organization.projects.find((p) => p.id === entry?.projectId);
  const projectRef = useMemo(
    () => (project ? { id: project.id, slug: project.slug } : undefined),
    [project],
  );

  const stored = useToolMetadata(server?.id, {
    enabled: remote,
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

  // Only a remote server with nothing stored opens a live session, the way
  // the Inspect tab does; one that lists records its tools as it goes.
  const nothingStored =
    remote && !stored.isLoading && !stored.isError && storedTools.length === 0;
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
  // Connect on the Gram origin: a custom domain would need its own session.
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
  const liveTools = useMemo(
    () =>
      server && live.tools
        ? proxiedToolsToServerTools(server.id, live.tools)
        : undefined,
    [server, live.tools],
  );

  if (!server) return { status: "none" };
  if (!server.dynamicTools) return { status: "ready", tools: server.tools };
  if (!server.remoteBacked) return { status: "dynamic" };
  if (stored.isLoading) return { status: "loading" };
  if (stored.isError) return { status: "error", retry: stored.refetch };
  if (storedTools.length > 0) return { status: "ready", tools: storedTools };
  if (!canRecordTools) return { status: "needs-write" };
  if (mcpServer.isError || endpoints.isError) {
    return {
      status: "error",
      retry: () => {
        void mcpServer.refetch();
        void endpoints.refetch();
      },
    };
  }
  if (mcpServer.isLoading || endpoints.isLoading) return { status: "loading" };
  // No Gram-origin endpoint: nothing to list through or connect to.
  if (!platformSlug) return { status: "needs-connect", connect: undefined };
  if (live.loading) return { status: "loading" };
  if (live.needsAuth) return { status: "needs-connect", connect: live.connect };
  if (live.isError) return { status: "error", retry: live.refetch };
  return { status: "ready", tools: liveTools ?? [] };
}
