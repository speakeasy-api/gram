import {
  HeadersEditor,
  type EditableHeader,
  type HeaderWriteFields,
  type HeadersEditorAdapter,
} from "@/components/headers-editor";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { Type } from "@/components/ui/type";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useCreateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import { useDeleteRemoteMcpServerHeaderMutation } from "@gram/client/react-query/deleteRemoteMcpServerHeader.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import {
  invalidateAllRemoteMcpServerHeaders,
  useRemoteMcpServerHeaders,
} from "@gram/client/react-query/remoteMcpServerHeaders.js";
import { useUpdateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { Alert, Badge, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowRight } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";

// A single remote_mcps row can back several mcp_servers rows. Its headers are
// stored on the remote, so editing them from any one MCP server silently
// rewrites the values every sibling server sends. HeadersSectionContext tells
// the component which surface it's rendered from so it can guard against that:
//  - "mcp-server": rendered on an MCP server's Settings tab. When the backing
//    remote is shared by more than one server, editing is locked and the user
//    is pointed at the Remote MCP source, the single canonical edit surface.
//  - "remote-mcp": rendered on the Remote MCP source page. Always editable,
//    with an indicator listing every MCP server the change will affect.
export type HeadersSectionContext =
  | { kind: "mcp-server" }
  | { kind: "remote-mcp"; linkedMcpServers: McpServer[] };

export function HeadersSection({
  remoteMcpServerId,
  context,
}: {
  remoteMcpServerId: string;
  context: HeadersSectionContext;
}): JSX.Element {
  const routes = useRoutes();

  // On an MCP server's Settings tab we need to know whether the backing remote
  // is shared before deciding to lock editing. On the Remote MCP page the
  // caller already knows the linked servers, so skip the extra fetch there.
  const isMcpServerContext = context.kind === "mcp-server";
  const siblingsQuery = useMcpServers({ remoteMcpServerId }, undefined, {
    enabled: isMcpServerContext && remoteMcpServerId !== "",
  });
  const linkedMcpServers = useMemo(() => {
    if (context.kind === "remote-mcp") return context.linkedMcpServers;
    return (siblingsQuery.data?.mcpServers ?? []).filter(
      (server) => server.remoteMcpServerId === remoteMcpServerId,
    );
  }, [context, siblingsQuery.data, remoteMcpServerId]);

  const sharedByOthers = linkedMcpServers.length > 1;
  const readOnly = isMcpServerContext && sharedByOthers;
  const siblingsLoading = isMcpServerContext && siblingsQuery.isLoading;

  const headersQuery = useRemoteMcpServerHeaders(
    { remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "" },
  );

  const queryClient = useQueryClient();
  const createHeader = useCreateRemoteMcpServerHeaderMutation();
  const updateHeader = useUpdateRemoteMcpServerHeaderMutation();
  const deleteHeader = useDeleteRemoteMcpServerHeaderMutation();

  const adapter: HeadersEditorAdapter = {
    headers: headersQuery.data?.headers,
    isLoading: headersQuery.isLoading,
    isSaving:
      createHeader.isPending ||
      updateHeader.isPending ||
      deleteHeader.isPending,
    mutationError:
      createHeader.error ?? updateHeader.error ?? deleteHeader.error,
    createHeader: async (fields: HeaderWriteFields) => {
      await createHeader.mutateAsync({
        request: { createServerHeaderForm: { remoteMcpServerId, ...fields } },
      });
    },
    updateHeader: async (id: string, fields: HeaderWriteFields) => {
      await updateHeader.mutateAsync({
        request: { updateServerHeaderForm: { id, ...fields } },
      });
    },
    deleteHeader: async (id: string) => {
      await deleteHeader.mutateAsync({ request: { id } });
    },
    refetch: async (): Promise<EditableHeader[] | null> => {
      const refreshed = await headersQuery.refetch();
      if (refreshed.isError || !refreshed.data) return null;
      return refreshed.data.headers ?? [];
    },
    invalidate: async () => {
      await invalidateAllRemoteMcpServerHeaders(queryClient, {
        refetchType: "all",
      });
    },
  };

  const remoteSettingsHref = `${routes.sources.source.href(
    "remotemcp",
    remoteMcpServerId,
  )}#settings`;

  const aboveContent = (
    <>
      {readOnly ? (
        <Alert variant="warning" dismissible={false}>
          <Stack gap={2}>
            <Type small>
              These headers are shared by {linkedMcpServers.length} MCP servers
              backed by this remote source. Editing them here would change the
              values every one of those servers sends, so editing is disabled on
              this page.
            </Type>
            <Link
              to={remoteSettingsHref}
              className="text-primary inline-flex items-center gap-1 text-sm hover:underline"
            >
              Edit on the Remote MCP source
              <ArrowRight className="size-3.5" />
            </Link>
          </Stack>
        </Alert>
      ) : null}

      {context.kind === "remote-mcp" && linkedMcpServers.length > 0 ? (
        <Alert variant="warning" dismissible={false}>
          <Stack gap={1}>
            <Type small>
              Changes here affect {linkedMcpServers.length}{" "}
              {linkedMcpServers.length === 1 ? "MCP server" : "MCP servers"}{" "}
              backed by this source:
            </Type>
            <div className="flex flex-wrap gap-2">
              {linkedMcpServers.map((server) => (
                <Link
                  key={server.id}
                  to={routes.mcp.x.overview.href(mcpServerRouteParam(server))}
                  className="no-underline"
                >
                  <Badge variant="neutral" className="hover:bg-muted">
                    <Badge.Text>{server.name || "MCP Server"}</Badge.Text>
                  </Badge>
                </Link>
              ))}
            </div>
          </Stack>
        </Alert>
      ) : null}
    </>
  );

  return (
    <HeadersEditor
      adapter={adapter}
      title="Upstream Headers"
      description="Headers sent to the remote MCP URL."
      readOnly={readOnly}
      loading={siblingsLoading}
      aboveContent={aboveContent}
    />
  );
}
