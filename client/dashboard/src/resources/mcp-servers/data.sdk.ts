import {
  listedMcpServers,
  mcpServerDisplayName,
} from "@/components/mcp/mcp-server-kind";
import { useProject } from "@/contexts/Auth";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useToolset } from "@/hooks/toolTypes";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import {
  invalidateMcpServerQueries,
  mcpServerVisibilityUpdateForm,
} from "@/lib/mcp-server-visibility";
import { getMcpServerArgs } from "@/lib/sources";
import {
  pluginServerRef,
  serverMatchesRef,
} from "@/pages/mcp/overview/serverMatchesRef";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import { useDeleteMcpServerMutation } from "@gram/client/react-query/deleteMcpServer.js";
import { useDeleteMetaMcpServerMutation } from "@gram/client/react-query/deleteMetaMcpServer.js";
import {
  buildGetMcpServerQuery,
  useGetMcpServer,
} from "@gram/client/react-query/getMcpServer.js";
import { useGetMetaMcpServer } from "@gram/client/react-query/getMetaMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import {
  invalidateAllMetaMcpServers,
  useMetaMcpServers,
} from "@gram/client/react-query/metaMcpServers.js";
import { invalidateAllGetMetaMcpServer } from "@gram/client/react-query/getMetaMcpServer.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { useUpdateMetaMcpServerMutation } from "@gram/client/react-query/updateMetaMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import type { ActionHandlers, Query, ResourceData } from "../kit/types";
import {
  GATEWAY_PREFIX,
  isGatewayId,
  type McpServerAvailability,
  type McpServerRecord,
  type McpServerRow,
  type McpServerSource,
} from "./model";

// ---------------------------------------------------------------------------
// Mapping SDK records into the resource's own model
// ---------------------------------------------------------------------------

function serverSource(server: McpServer): Exclude<McpServerSource, "gateway"> {
  if (server.unproxiedMcpServerId) return "unproxied";
  if (server.tunneledMcpServerId) return "tunneled";
  if (server.remoteMcpServerId) return "remote";
  return "hosted";
}

const SOURCE_SUMMARY: Record<Exclude<McpServerSource, "gateway">, string> = {
  hosted: "Hosted by Speakeasy",
  remote: "A remote server Speakeasy proxies",
  tunneled: "Reached through a private tunnel",
  unproxied: "Agents connect to it directly",
};

function availability(value: string): McpServerAvailability {
  return value === "disabled" || value === "public" ? value : "private";
}

function serverRow(server: McpServer): McpServerRow {
  const name = mcpServerDisplayName(server);
  const source = serverSource(server);
  return {
    id: server.slug ?? server.id,
    serverId: server.id,
    name,
    source,
    summary:
      source === "hosted" &&
      server.toolset?.name &&
      server.toolset.name !== name
        ? `Hosted from ${server.toolset.name}`
        : SOURCE_SUMMARY[source],
    availability: availability(server.visibility),
    toolCount: server.toolset?.toolCount,
    mark: { label: name },
    updatedAt: server.updatedAt,
  };
}

function gatewayRow(gateway: MetaMcpServer): McpServerRow {
  const count = gateway.memberCount ?? 0;
  return {
    id: GATEWAY_PREFIX + gateway.id,
    serverId: gateway.id,
    name: gateway.name,
    source: "gateway",
    summary:
      count === 0
        ? "Fronts no servers yet"
        : `Fronts ${count} ${count === 1 ? "server" : "servers"} at one URL`,
    availability: availability(gateway.visibility),
    mark: { label: gateway.name },
    updatedAt: gateway.updatedAt,
  };
}

/** In a plugin. Hosted entries can be keyed by toolset, like the MCP pages match them. */
function isDistributed(
  plugins: Plugin[] | undefined,
  match:
    | {
        server: McpServer;
        toolset?: { id: string; slug: string; name: string };
      }
    | { metaMcpServerId: string },
): boolean {
  const ref =
    "server" in match ? pluginServerRef(match.server, match.toolset) : null;
  return (plugins ?? []).some((plugin) =>
    (plugin.servers ?? []).some((s) =>
      ref
        ? serverMatchesRef(s, ref)
        : "metaMcpServerId" in match &&
          s.metaMcpServerId === match.metaMcpServerId,
    ),
  );
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : "Something went wrong.";
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

function useList(): Query<McpServerRow[]> {
  const gramProject = useProjectSlugForRequests();
  const servers = useMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });
  const gateways = useMetaMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });

  if (servers.isPending || gateways.isPending) return { status: "loading" };
  if (servers.isError || gateways.isError) {
    return {
      status: "error",
      message: errorText(servers.error ?? gateways.error),
      retry: () => {
        void servers.refetch();
        void gateways.refetch();
      },
    };
  }
  return {
    status: "ready",
    data: [
      ...(gateways.data.metaMcpServers ?? []).map(gatewayRow),
      ...listedMcpServers(servers.data.mcpServers ?? []).map(({ server }) =>
        serverRow(server),
      ),
    ],
  };
}

function useRecord(id: string): Query<McpServerRecord> {
  const gramProject = useProjectSlugForRequests();
  const gateway = isGatewayId(id);
  const gatewayId = gateway ? id.slice(GATEWAY_PREFIX.length) : "";

  // Both branches' hooks run every render; `enabled` picks the live one.
  const server = useGetMcpServer(
    { ...getMcpServerArgs(id), gramProject },
    undefined,
    { enabled: !gateway, throwOnError: false },
  );
  const meta = useGetMetaMcpServer({ id: gatewayId, gramProject }, undefined, {
    enabled: gateway,
    throwOnError: false,
  });
  const members = useMetaMcpMembers(
    { metaMcpServerId: gatewayId, gramProject },
    undefined,
    { enabled: gateway, throwOnError: false },
  );
  const serverId = gateway ? undefined : server.data?.id;
  const endpoints = useMcpEndpoints(
    gateway
      ? { metaMcpServerId: gatewayId, gramProject }
      : { mcpServerId: serverId, gramProject },
    undefined,
    { enabled: gateway || serverId != null, throwOnError: false },
  );
  const toolsetSlug = gateway ? undefined : server.data?.toolset?.slug;
  const toolset = useToolset(toolsetSlug, {
    enabled: toolsetSlug != null,
    throwOnError: false,
  });
  const plugins = usePlugins({ gramProject }, undefined, {
    throwOnError: false,
  });
  const { mcpUrl } = useResolvedMcpServerUrl(
    endpoints.data?.mcpEndpoints ?? [],
    endpoints.isPending,
  );

  const primary = gateway ? meta : server;
  if (primary.isPending) return { status: "loading" };
  if (primary.isError || !primary.data) {
    return {
      status: "error",
      message: errorText(primary.error),
      retry: () => void primary.refetch(),
    };
  }

  if (gateway && meta.data) {
    // An unknown member list must not read as "fronts no servers".
    if (members.isPending) return { status: "loading" };
    if (members.isError) {
      return {
        status: "error",
        message: errorText(members.error),
        retry: () => void members.refetch(),
      };
    }
    const row = gatewayRow(meta.data);
    const memberList = members.data.members ?? [];
    return {
      status: "ready",
      data: {
        ...row,
        toolCount: undefined,
        url: mcpUrl,
        tools: [],
        members: memberList.map((m) => ({
          id: m.mcpServerSlug ?? m.mcpServerId,
          name: m.mcpServerName ?? m.mcpServerSlug ?? "Unnamed server",
        })),
        distributed: isDistributed(plugins.data?.plugins, {
          metaMcpServerId: meta.data.id,
        }),
        used: true,
        createdAt: meta.data.createdAt,
      },
    };
  }

  const s = server.data!;
  const row = serverRow(s);
  return {
    status: "ready",
    data: {
      ...row,
      toolsetId: s.toolsetId ?? undefined,
      url: mcpUrl,
      tools: (toolset.data?.tools ?? []).map((tool) => ({
        name: tool.name,
        description: tool.description,
      })),
      members: [],
      distributed: isDistributed(plugins.data?.plugins, {
        server: s,
        toolset: toolset.data,
      }),
      // The setup guide is marked complete once someone has connected.
      used: s.setupGuideCompletedAt != null,
      createdAt: s.createdAt,
    },
  };
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

function useListActions(): ActionHandlers {
  const { hasScope } = useRBAC();
  if (!hasScope("mcp:write")) return {};
  // Creating a server is a conversation; the kit routes it to chat.
  return { create: { run: () => {}, pending: false } };
}

function useRecordActions(rec: McpServerRecord | undefined): ActionHandlers {
  const { hasScope } = useRBAC();
  const project = useProject();
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const gramProject = useProjectSlugForRequests();
  const [settingAvailability, setSettingAvailability] = useState(false);

  const updateServer = useUpdateMcpServerMutation();
  const updateGateway = useUpdateMetaMcpServerMutation();
  const deleteServer = useDeleteMcpServerMutation();
  const deleteGateway = useDeleteMetaMcpServerMutation();

  if (!rec) return {};
  const gateway = isGatewayId(rec.id);
  const rawId = rec.serverId;
  // Checked like the API does: a hosted server is granted under its toolset id.
  const canWrite = hasScope("mcp:write", rec.toolsetId ?? rawId);

  const refresh = async () => {
    await invalidateMcpServerQueries(queryClient);
    await invalidateAllMetaMcpServers(queryClient);
    await invalidateAllGetMetaMcpServer(queryClient);
  };

  /** Servers: the update replaces the whole record, so read the latest first. */
  const writeServer = async (change: {
    name?: string;
    availability?: McpServerAvailability;
  }) => {
    const latest = await queryClient.fetchQuery({
      ...buildGetMcpServerQuery(client, { id: rawId, gramProject }),
      staleTime: 0,
    });
    const form = mcpServerVisibilityUpdateForm(
      latest,
      change.availability ?? latest.visibility,
    );
    await updateServer.mutateAsync({
      request: {
        gramProject,
        updateMcpServerForm: { ...form, name: change.name ?? form.name },
      },
    });
  };

  const writeGateway = async (change: {
    name?: string;
    availability?: McpServerAvailability;
  }) => {
    await updateGateway.mutateAsync({
      request: {
        gramProject,
        updateMetaMcpServerForm: {
          id: rawId,
          name: change.name ?? rec.name,
          visibility:
            change.availability === "public"
              ? undefined
              : (change.availability ?? undefined),
        },
      },
    });
  };

  const write = (
    change: { name?: string; availability?: McpServerAvailability },
    success: string,
  ) => {
    setSettingAvailability(true);
    void (gateway ? writeGateway(change) : writeServer(change))
      .then(refresh)
      .then(() => toast.success(success))
      .catch((error: unknown) =>
        toast.error(`Couldn't save the change. ${errorText(error)}`),
      )
      .finally(() => setSettingAvailability(false));
  };

  if (!canWrite) {
    return {
      ask: { run: () => {}, pending: false },
      tryIt: { run: () => {}, pending: false },
    };
  }

  const sourceBacked =
    rec.source === "remote" ||
    rec.source === "tunneled" ||
    rec.source === "unproxied";

  return {
    ask: { run: () => {}, pending: false },
    tryIt: { run: () => {}, pending: false },
    distribute: hasScope("plugin:write", project.id)
      ? { run: () => {}, pending: false }
      : undefined,
    turnOn: {
      pending: settingAvailability,
      run: () => write({ availability: "private" }, "Server turned on"),
    },
    setAvailability: {
      pending: settingAvailability,
      run: (value) =>
        write(
          { availability: availability(value ?? "private") },
          "Availability changed",
        ),
    },
    rename: {
      pending: settingAvailability || updateGateway.isPending,
      run: (value) => {
        const name = value?.trim();
        if (name) write({ name }, "Server renamed");
      },
    },
    delete: {
      pending: deleteServer.isPending || deleteGateway.isPending,
      failed: deleteServer.isError || deleteGateway.isError,
      // Source-backed servers delete their source too, behind a typed
      // confirmation. The kit has no type-to-confirm kind yet, so the
      // classic page still owns that path.
      disabledReason: sourceBacked
        ? "Deleting this server also deletes its source. Use the server's settings page for now."
        : undefined,
      run: () => {
        const done = () => {
          void refresh();
          toast.success("Server deleted");
        };
        const failed = (error: unknown) => {
          toast.error(`Couldn't delete the server. ${errorText(error)}`);
        };
        if (gateway) {
          deleteGateway.mutate(
            { request: { id: rawId, gramProject } },
            { onSuccess: done, onError: failed },
          );
        } else {
          deleteServer.mutate(
            { request: { id: rawId, gramProject } },
            { onSuccess: done, onError: failed },
          );
        }
      },
    },
  };
}

export const mcpServersSdkData: ResourceData<McpServerRow, McpServerRecord> = {
  useList,
  useRecord,
  useListActions,
  useRecordActions,
};
