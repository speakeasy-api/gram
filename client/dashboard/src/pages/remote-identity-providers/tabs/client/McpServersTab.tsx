import { RequireScope } from "@/components/require-scope";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import { Type } from "@/components/ui/type";
import { useSlugs } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { formatRemoteMcpDisplay } from "@/lib/sources";
import type { OrganizationMcpServer } from "@gram/client/models/components/organizationmcpserver.js";
import {
  invalidateAllOrganizationRemoteSessionClientMcpServers,
  useOrganizationRemoteSessionClientMcpServers,
} from "@gram/client/react-query/organizationRemoteSessionClientMcpServers.js";
import { useRemoveOrganizationRemoteSessionClientFromMcpServerMutation } from "@gram/client/react-query/removeOrganizationRemoteSessionClientFromMcpServer.js";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, Network } from "lucide-react";
import { toast } from "sonner";

// mcpServerHref builds a cross-project link to a remote-MCP server's detail page
// from the org-admin context. The org context has no project slug in its own
// URL, so we build the project-scoped path from the server's owning project slug
// (returned by the API) rather than a route helper, which can only resolve the
// current project. Returns null when the slug is unavailable.
function mcpServerHref(
  orgSlug: string | undefined,
  server: OrganizationMcpServer,
): string | null {
  if (!orgSlug || !server.projectSlug) return null;
  const param = server.slug?.trim() || server.id;
  return `/${orgSlug}/projects/${server.projectSlug}/mcp/x/${param}/overview`;
}

export function McpServersTab({ clientId }: { clientId: string }): JSX.Element {
  const { orgSlug } = useSlugs();
  const queryClient = useQueryClient();
  const { data, isLoading, isError } =
    useOrganizationRemoteSessionClientMcpServers({
      clientId,
    });
  const remove = useRemoveOrganizationRemoteSessionClientFromMcpServerMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionClientMcpServers(
        queryClient,
        { refetchType: "all" },
      );
      toast.success("Removed client from MCP server");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to remove client from MCP server",
      );
    },
  });

  const items = data?.items ?? [];

  if (isError) {
    return (
      <Type className="text-destructive py-8 text-center">
        Failed to load attached MCP servers.
      </Type>
    );
  }

  if (!isLoading && items.length === 0) {
    return (
      <Type muted className="py-8 text-center">
        This client is not attached to any MCP servers.
      </Type>
    );
  }

  return (
    <DotTable headers={[{ label: "MCP Server" }, { label: "" }]}>
      {items.map((server) => {
        const href = mcpServerHref(orgSlug, server);
        const label = formatRemoteMcpDisplay({
          name: server.name,
          url: server.url ?? "",
        });
        return (
          <DotRow
            key={server.id}
            icon={<Network className="text-muted-foreground h-5 w-5" />}
            href={href ?? undefined}
            ariaLabel={href ? `View MCP server ${label}` : undefined}
          >
            <td className="px-3 py-3">
              <Type
                variant="subheading"
                as="div"
                className={cn(
                  "truncate text-sm",
                  href &&
                    "group-hover:text-primary transition-colors group-hover:underline",
                )}
              >
                {label}
              </Type>
            </td>
            <td className="px-3 py-3 text-right">
              <RequireScope scope="org:admin" level="section">
                <div
                  className="relative z-20"
                  onClick={(e) => e.stopPropagation()}
                >
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="tertiary" size="sm">
                        <Button.LeftIcon>
                          <MoreHorizontal className="h-4 w-4" />
                        </Button.LeftIcon>
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem
                        disabled={remove.isPending}
                        onClick={() =>
                          remove.mutate({
                            request: {
                              removeClientFromMcpServerRequestBody: {
                                clientId,
                                mcpServerId: server.id,
                              },
                            },
                          })
                        }
                      >
                        Remove from server
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </RequireScope>
            </td>
          </DotRow>
        );
      })}
    </DotTable>
  );
}
