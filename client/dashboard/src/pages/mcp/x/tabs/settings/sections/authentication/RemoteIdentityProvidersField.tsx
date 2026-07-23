import { AssetImage } from "@/components/asset-image";
import { RequireScope } from "@/components/require-scope";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Type } from "@/components/ui/type";
import {
  formatRemoteSessionIssuerDisplay,
  remoteSessionScopeTier,
} from "@/lib/sources";
import { ScopeBadge } from "@/pages/remote-identity-providers/ScopeBadge";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { Button } from "@speakeasy-api/moonshine";
import { Plus, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import { SettingsInlineEmptyState } from "../../SettingsInlineEmptyState";

export function RemoteIdentityProvidersField({
  associatedIssuers,
  isLoading,
  onAdd,
  onEdit,
  onDelete,
}: {
  associatedIssuers: RemoteSessionIssuer[];
  isLoading: boolean;
  onAdd: () => void;
  onEdit: (issuer: RemoteSessionIssuer) => void;
  onDelete: (issuer: RemoteSessionIssuer) => void;
}): JSX.Element {
  let providerControls: ReactNode;
  if (isLoading) {
    providerControls = (
      <Type muted small>
        Loading...
      </Type>
    );
  } else if (associatedIssuers.length === 0) {
    providerControls = (
      <SettingsInlineEmptyState
        title="No remote identity providers"
        description="Attach a provider if the upstream service requires users to sign in to access their data."
        action={
          <RequireScope scope="mcp:write" level="component">
            <Button variant="secondary" onClick={onAdd}>
              <Button.LeftIcon>
                <Plus className="size-4" />
              </Button.LeftIcon>
              <Button.Text>Attach Provider</Button.Text>
            </Button>
          </RequireScope>
        }
      />
    );
  } else {
    providerControls = (
      <div className="space-y-3">
        {associatedIssuers.map((issuer) => (
          <RemoteIdentityProviderRow
            key={issuer.id}
            issuer={issuer}
            onEdit={() => onEdit(issuer)}
            onDelete={() => onDelete(issuer)}
          />
        ))}
      </div>
    );
  }

  return (
    <Field>
      <FieldLabel>Remote Identity Providers</FieldLabel>
      {providerControls}
      <FieldDescription>
        Upstream identity providers for services that require per-user
        authorization.
      </FieldDescription>
    </Field>
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
  // Editing rewrites the issuer's own metadata, which only the owning tenant can
  // do. Organization-level and platform issuers are managed on their own admin
  // surfaces (and the project-scoped update endpoint rejects them), so offer Edit
  // only for a project-owned issuer. Delete stays available for every tier: it
  // detaches the tenant's own client, never the shared issuer.
  const canEdit = remoteSessionScopeTier(issuer) === "project";

  return (
    <div className="rounded-md border p-3">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        {issuer.logoAssetId ? (
          <AssetImage
            assetId={issuer.logoAssetId}
            className="size-8 shrink-0 rounded"
          />
        ) : null}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <Type small className="truncate font-medium">
              {formatRemoteSessionIssuerDisplay(issuer)}
            </Type>
            <ScopeBadge
              projectId={issuer.projectId}
              organizationId={issuer.organizationId}
            />
          </div>
          <Type muted mono variant="small" className="break-all">
            {issuer.issuer}
          </Type>
        </div>
        <RequireScope scope="mcp:write" level="component">
          <div className="flex shrink-0 items-center gap-2">
            {canEdit && (
              <Button size="md" variant="secondary" onClick={onEdit}>
                <Button.Text>Edit</Button.Text>
              </Button>
            )}
            <Button
              size="md"
              variant="destructive-secondary"
              onClick={onDelete}
            >
              <Button.LeftIcon>
                <Trash2 className="size-4" />
              </Button.LeftIcon>
              <Button.Text>Delete</Button.Text>
            </Button>
          </div>
        </RequireScope>
      </div>
    </div>
  );
}
