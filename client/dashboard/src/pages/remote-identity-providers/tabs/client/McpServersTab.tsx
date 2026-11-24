import { RequireScope } from "@/components/require-scope";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import type { Action } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";
import { useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { MCP_AUTHENTICATION_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection";
import { cn } from "@/lib/utils";
import { formatRemoteMcpDisplay, mcpServerRouteParam } from "@/lib/sources";
import type { OrganizationMcpServer } from "@gram/client/models/components/organizationmcpserver.js";
import { useOrganizationRemoteSessionClientMcpServers } from "@gram/client/react-query/organizationRemoteSessionClientMcpServers.js";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { MoreHorizontal } from "lucide-react";
import { useNavigate } from "react-router";

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
  return `/${orgSlug}/projects/${server.projectSlug}/mcp/${mcpServerRouteParam(server)}/overview`;
}

// mcpServerIdentityHref points at the server's own Sign-in settings, where an
// organization served by typed auth clients changes what the server offers.
function mcpServerIdentityHref(
  orgSlug: string | undefined,
  server: OrganizationMcpServer,
): string | null {
  if (!orgSlug || !server.projectSlug) return null;
  const settings = `/${orgSlug}/projects/${server.projectSlug}/mcp/${mcpServerRouteParam(server)}/settings`;
  // Empty URLs can describe either a tunnel or a legacy hosted usage row.
  return server.url ? `${settings}#${MCP_AUTHENTICATION_SECTION_ID}` : settings;
}

export function McpServersTab({ clientId }: { clientId: string }): JSX.Element {
  const { orgSlug } = useSlugs();
  const { hasAnyScope } = useRBAC();
  const canManage = hasAnyScope(["org:admin"]);
  const navigate = useNavigate();
  const { data, isLoading, isError } =
    useOrganizationRemoteSessionClientMcpServers({
      clientId,
    });
  const items = data?.items ?? [];

  if (isError) {
    return (
      <Text className="text-destructive py-8 text-center">
        Failed to load MCP servers using this credential.
      </Text>
    );
  }

  if (!isLoading && items.length === 0) {
    return (
      <Text muted className="py-8 text-center">
        No MCP servers use this credential.
      </Text>
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
        const identityHref = mcpServerIdentityHref(orgSlug, server);
        const actions: Action[] = identityHref
          ? [
              {
                label: server.url
                  ? "Manage in Sign-in"
                  : "Open server settings",
                onClick: () => void navigate(identityHref),
              },
            ]
          : [];
        return (
          <TableRowContextMenu
            key={server.id}
            actions={canManage ? actions : []}
          >
            <DotRow
              icon={
                <Icon
                  name="network"
                  className="text-muted-foreground h-5 w-5"
                />
              }
              href={href ?? undefined}
              ariaLabel={href ? `View MCP server ${label}` : undefined}
            >
              <td className="px-3 py-3">
                <Text
                  variant="subheading"
                  as="div"
                  className={cn(
                    "truncate text-sm",
                    href &&
                      "group-hover:text-primary transition-colors group-hover:underline",
                  )}
                >
                  {label}
                </Text>
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
                        {actions.map((action, index) => (
                          <DropdownMenuItem
                            key={index}
                            disabled={action.disabled}
                            onClick={() => action.onClick()}
                          >
                            {action.label}
                          </DropdownMenuItem>
                        ))}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </RequireScope>
              </td>
            </DotRow>
          </TableRowContextMenu>
        );
      })}
    </DotTable>
  );
}
