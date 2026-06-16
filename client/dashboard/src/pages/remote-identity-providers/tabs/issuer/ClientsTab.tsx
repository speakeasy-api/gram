import { RequireScope } from "@/components/require-scope";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import { Type } from "@/components/ui/type";
import { useOrgRoutes } from "@/routes";
import type { OrganizationRemoteSessionClient } from "@gram/client/models/components";
import { useOrganizationRemoteSessionClients } from "@gram/client/react-query/index.js";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
} from "@speakeasy-api/moonshine";
import { MoreHorizontal } from "lucide-react";
import { useState } from "react";
import { DeleteClientDialog } from "../../clientDialogs";

export function ClientsTab({ issuerId }: { issuerId: string }): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const { data, isLoading, isError } = useOrganizationRemoteSessionClients({
    issuerId,
  });
  const [deleteTarget, setDeleteTarget] =
    useState<OrganizationRemoteSessionClient | null>(null);

  const items = data?.result.items ?? [];

  if (isError) {
    return (
      <Type className="text-destructive py-8 text-center">
        Failed to load clients.
      </Type>
    );
  }

  if (!isLoading && items.length === 0) {
    return (
      <Type muted className="py-8 text-center">
        No clients registered with this provider.
      </Type>
    );
  }

  return (
    <>
      <DotTable
        headers={[
          { label: "Client ID" },
          { label: "MCP Servers" },
          { label: "Active Sessions" },
          { label: "" },
        ]}
      >
        {items.map((item) => (
          <DotRow
            key={item.client.id}
            icon={<Icon name="key" className="text-muted-foreground h-5 w-5" />}
            href={orgRoutes.remoteIdentityProviders.clientDetail.href(
              issuerId,
              item.client.id,
            )}
            ariaLabel={`View client ${item.client.clientId}`}
          >
            <td className="px-3 py-3">
              <Type
                variant="subheading"
                as="div"
                className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
              >
                {item.client.clientId}
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
                      <DropdownMenuItem onClick={() => setDeleteTarget(item)}>
                        Delete client
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </RequireScope>
            </td>
          </DotRow>
        ))}
      </DotTable>

      {deleteTarget && (
        <DeleteClientDialog
          clientId={deleteTarget.client.id}
          clientLabel={deleteTarget.client.clientId}
          onClose={() => setDeleteTarget(null)}
        />
      )}
    </>
  );
}
