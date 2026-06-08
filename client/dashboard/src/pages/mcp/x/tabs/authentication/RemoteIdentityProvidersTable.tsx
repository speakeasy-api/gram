import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import type { RemoteSessionIssuer } from "@gram/client/models/components";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { Lock, Plus, Trash2 } from "lucide-react";

export function RemoteIdentityProvidersTable({
  associatedIssuers,
  isLoading,
  onAdd,
  onEdit,
  onDelete,
  attachDisabledReason,
}: {
  associatedIssuers: RemoteSessionIssuer[];
  isLoading: boolean;
  onAdd: () => void;
  onEdit: (issuer: RemoteSessionIssuer) => void;
  onDelete: (issuer: RemoteSessionIssuer) => void;
  // When set, the Attach button renders disabled with this string as its
  // tooltip. Used to surface the temporary single-provider constraint until
  // multi-client support lands.
  attachDisabledReason?: string;
}): JSX.Element {
  return (
    <section>
      <Heading variant="h4" className="mb-3">
        Remote Identity Providers
      </Heading>
      <Type muted small className="mb-4">
        Upstream identity providers users authenticate against to access MCP
        Server functionality.
      </Type>
      {isLoading ? (
        <Type muted small>
          Loading…
        </Type>
      ) : (
        <Stack gap={3}>
          {associatedIssuers.length === 0 ? (
            <Stack direction="horizontal" gap={2} align="center">
              <Lock className="text-muted-foreground size-4" />
              <Type className="font-medium">
                No remote identity providers configured yet.
              </Type>
            </Stack>
          ) : (
            associatedIssuers.map((issuer) => (
              <RemoteIdentityProviderRow
                key={issuer.id}
                issuer={issuer}
                onEdit={() => onEdit(issuer)}
                onDelete={() => onDelete(issuer)}
              />
            ))
          )}
          <div>
            <RequireScope scope="mcp:write" level="component">
              {attachDisabledReason ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    {/* Disabled native buttons don't fire pointer events, so
                        the tooltip never opens on hover without a wrapper.
                        Matches the EmptyAuthenticationState pattern. */}
                    <span
                      role="button"
                      aria-disabled="true"
                      tabIndex={0}
                      aria-label={attachDisabledReason}
                    >
                      <Button variant="secondary" disabled>
                        <Button.LeftIcon>
                          <Plus className="size-4" />
                        </Button.LeftIcon>
                        <Button.Text>
                          Attach Remote Identity Provider
                        </Button.Text>
                      </Button>
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>{attachDisabledReason}</TooltipContent>
                </Tooltip>
              ) : (
                <Button variant="secondary" onClick={onAdd}>
                  <Button.LeftIcon>
                    <Plus className="size-4" />
                  </Button.LeftIcon>
                  <Button.Text>Attach Remote Identity Provider</Button.Text>
                </Button>
              )}
            </RequireScope>
          </div>
        </Stack>
      )}
    </section>
  );
}

function RemoteIdentityProviderRow({
  issuer,
  onEdit,
  onDelete,
}: {
  issuer: RemoteSessionIssuer;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="rounded-md border p-3">
      <Stack direction="horizontal" gap={2} align="center">
        <Stack gap={0} className="min-w-0 flex-1">
          <Type small className="truncate font-mono">
            {issuer.slug}
          </Type>
          <Type muted mono variant="small" className="break-all">
            {issuer.issuer}
          </Type>
        </Stack>
        <RequireScope scope="mcp:write" level="component">
          <Button size="md" variant="secondary" onClick={onEdit}>
            <Button.Text>Edit</Button.Text>
          </Button>
          <Button size="md" variant="destructive-secondary" onClick={onDelete}>
            <Button.LeftIcon>
              <Trash2 className="size-4" />
            </Button.LeftIcon>
            <Button.Text>Delete</Button.Text>
          </Button>
        </RequireScope>
      </Stack>
    </div>
  );
}
