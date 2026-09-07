import { AssetImage } from "@/components/asset-image";
import { RequireScope } from "@/components/require-scope";
import { Text } from "@/components/ui/Text";
import { remoteSessionScopeTier } from "@/lib/sources";
import { IssuerLink } from "@/pages/remote-identity-providers/IssuerLink";
import { ScopeBadge } from "@/pages/remote-identity-providers/ScopeBadge";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { Button } from "@/components/ui/Button";
import { Plus, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import { AuthRow, ExplainerDialog } from "./AuthRow";

export function RemoteIdentityProvidersField({
  associatedIssuers,
  isLoading,
  allowAdditionalProviders,
  projectId,
  onAdd,
  onEdit,
  onDelete,
}: {
  associatedIssuers: RemoteSessionIssuer[];
  isLoading: boolean;
  /** Gateways bind a provider per member; remote/tunneled servers have one
   * upstream, so only their empty state may offer an attach. */
  allowAdditionalProviders: boolean;
  /** Scopes the mcp:write gates to the target's own project. */
  projectId: string;
  onAdd: () => void;
  onEdit: (issuer: RemoteSessionIssuer) => void;
  onDelete: (issuer: RemoteSessionIssuer) => void;
}): JSX.Element {
  const addButton = (
    <RequireScope scope="mcp:write" resourceId={projectId} level="component">
      <Button variant="secondary" size="md" onClick={onAdd}>
        <Button.LeftIcon>
          <Plus className="size-4" />
        </Button.LeftIcon>
        <Button.Text>Add provider</Button.Text>
      </Button>
    </RequireScope>
  );

  let providerControls: ReactNode;
  if (isLoading) {
    providerControls = (
      <Text muted small>
        Loading…
      </Text>
    );
  } else if (associatedIssuers.length === 0) {
    // The button is the empty state: "None yet." beside it says nothing the
    // absent list does not already say.
    providerControls = addButton;
  } else {
    providerControls = (
      <div className="space-y-3">
        {associatedIssuers.map((issuer) => (
          <RemoteIdentityProviderRow
            key={issuer.id}
            issuer={issuer}
            projectId={projectId}
            onEdit={() => onEdit(issuer)}
            onDelete={() => onDelete(issuer)}
          />
        ))}
        {allowAdditionalProviders && addButton}
      </div>
    );
  }

  return (
    <AuthRow
      label="Connected services"
      hint={
        <>
          Rarely needed — only when the upstream service makes each user sign in
          there themselves.
          <ExplainerDialog title="Connected services">
            <Text muted small className="block">
              Some servers act on a user&apos;s own data in another system —
              their Ashby account, their Linear workspace. That system decides
              whether this particular person may do it, so each user signs in
              there once and Speakeasy keeps that authorization alongside their
              session.
            </Text>
            <Text muted small className="block">
              Add a provider only when the upstream service asks every user to
              sign in for themselves. A server that reaches its API with one
              shared credential needs none.
            </Text>
          </ExplainerDialog>
        </>
      }
    >
      {providerControls}
    </AuthRow>
  );
}

function RemoteIdentityProviderRow({
  issuer,
  onEdit,
  onDelete,
  projectId,
}: {
  issuer: RemoteSessionIssuer;
  projectId: string;
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
    <div className="border p-3">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        {issuer.logoAssetId ? (
          <AssetImage
            assetId={issuer.logoAssetId}
            className="size-8 shrink-0"
          />
        ) : null}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <Text small className="truncate font-medium">
              <IssuerLink issuer={issuer} />
            </Text>
            <ScopeBadge
              projectId={issuer.projectId}
              organizationId={issuer.organizationId}
            />
          </div>
          <Text muted mono variant="small" className="break-all">
            {issuer.issuer}
          </Text>
        </div>
        <RequireScope
          scope="mcp:write"
          resourceId={projectId}
          level="component"
        >
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
              <Button.Text>Remove</Button.Text>
            </Button>
          </div>
        </RequireScope>
      </div>
    </div>
  );
}
