import { RequireScope } from "@/components/require-scope";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import type { Action } from "@/components/ui/more-actions";
import { Type } from "@/components/ui/type";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrgRoutes } from "@/routes";
import type { OrganizationRemoteSessionClient } from "@gram/client/models/components/organizationremotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useOrganizationRemoteSessionClients } from "@gram/client/react-query/organizationRemoteSessionClients.js";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
} from "@speakeasy-api/moonshine";
import { MoreHorizontal, Plus } from "lucide-react";
import { useState } from "react";
import { remoteSessionClientDisplayName } from "../../clientDisplay";
import { CreateRemoteSessionClientSheet } from "../../CreateRemoteSessionClientSheet";
import { DeleteClientDialog } from "../../clientDialogs";

export function ClientsTab({
  issuer,
}: {
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const { hasAnyScope } = useRBAC();
  const canManage = hasAnyScope(["org:admin"]);
  const { data, isLoading, isError } = useOrganizationRemoteSessionClients({
    issuerId: issuer.id,
  });
  const [deleteTarget, setDeleteTarget] =
    useState<OrganizationRemoteSessionClient | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

  const items = data?.result.items ?? [];

  let body: JSX.Element;
  if (isError) {
    body = (
      <Type className="text-destructive py-8 text-center">
        Failed to load clients.
      </Type>
    );
  } else if (!isLoading && items.length === 0) {
    body = (
      <Type muted className="py-8 text-center">
        No clients registered with this provider.
      </Type>
    );
  } else {
    body = (
      <DotTable
        headers={[
          { label: "Client ID" },
          { label: "MCP Servers" },
          { label: "Active Sessions" },
          { label: "" },
        ]}
      >
        {items.map((item) => {
          const actions: Action[] = [
            {
              label: "Delete client",
              destructive: true,
              onClick: () => setDeleteTarget(item),
            },
          ];
          return (
            <TableRowContextMenu
              key={item.client.id}
              actions={canManage ? actions : []}
            >
              <DotRow
                icon={
                  <Icon name="key" className="text-muted-foreground h-5 w-5" />
                }
                href={orgRoutes.remoteIdentityProviders.clientDetail.href(
                  issuer.id,
                  item.client.id,
                )}
                ariaLabel={`View client ${remoteSessionClientDisplayName(item.client)}`}
              >
                <td className="px-3 py-3">
                  <Type
                    variant="subheading"
                    as="div"
                    className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
                  >
                    {remoteSessionClientDisplayName(item.client)}
                  </Type>
                </td>
                <td className="px-3 py-3">
                  <Type small muted>
                    {item.mcpServerCount}{" "}
                    {item.mcpServerCount === 1 ? "server" : "servers"}
                  </Type>
                </td>
                <td className="px-3 py-3">
                  <Type small muted>
                    {item.activeSessionCount}{" "}
                    {item.activeSessionCount === 1 ? "session" : "sessions"}
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

  return (
    <>
      <div className="mb-4 flex items-center justify-end">
        <RequireScope scope="org:admin" level="component">
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Button.LeftIcon>
              <Plus />
            </Button.LeftIcon>
            <Button.Text>New Client</Button.Text>
          </Button>
        </RequireScope>
      </div>

      {body}

      {deleteTarget && (
        <DeleteClientDialog
          clientId={deleteTarget.client.id}
          clientLabel={remoteSessionClientDisplayName(deleteTarget.client)}
          onClose={() => setDeleteTarget(null)}
        />
      )}

      <CreateRemoteSessionClientSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
        issuer={issuer}
      />
    </>
  );
}
