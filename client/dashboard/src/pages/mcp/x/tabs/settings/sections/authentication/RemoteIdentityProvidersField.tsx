import { FieldError } from "@/components/ui/Field";
import { RequireScope } from "@/components/require-scope";
import { Text } from "@/components/ui/Text";
import { Skeleton } from "@/components/ui/Skeleton";
import { remoteSessionScopeTier } from "@/lib/sources";
import { IssuerLink, IssuerLogo, ScopeBadge } from "@/lib/remote-identity";
import { issuerDisplayName } from "@/pages/remote-identity-providers/issuerDisplay";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { Button } from "@/components/ui/Button";
import { Plus, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import { AuthRow, ExplainerDialog } from "./AuthRow";

/** Anchors the Authorization servers row, where a gateway links a member's sign-in. */
export const AUTHORIZATION_SERVERS_FIELD_ID = "authorization-servers";

export function RemoteIdentityProvidersField({
  associatedIssuers,
  isLoading,
  isError = false,
  allowAdditionalProviders,
  projectId,
  onAdd,
  onEdit,
  onDelete,
  readOnly = false,
  members,
  hiddenIssuerIds,
}: {
  associatedIssuers: RemoteSessionIssuer[];
  isLoading: boolean;
  /** An attached provider failed to load. Never rendered as the empty state,
   * which would tell the user nothing is connected; providers that did load
   * keep their rows. */
  isError?: boolean;
  /** Gateways bind a provider per member; remote/tunneled servers have one
   * upstream, so only their empty state may offer an attach. */
  allowAdditionalProviders: boolean;
  /** Scopes the mcp:write gates to the target's own project. */
  projectId: string;
  onAdd: () => void;
  onEdit: (issuer: RemoteSessionIssuer) => void;
  onDelete: (issuer: RemoteSessionIssuer) => void;
  readOnly?: boolean;
  /**
   * A gateway's members that sign people in, one row each, listed first: the
   * row is then about linking them, and a hand-added server is the rare case.
   */
  members?: ReactNode;
  /** Servers the member rows already show, so each appears once. */
  hiddenIssuerIds?: ReadonlySet<string>;
}): JSX.Element {
  const guided = members !== undefined;
  const others = hiddenIssuerIds
    ? associatedIssuers.filter((issuer) => !hiddenIssuerIds.has(issuer.id))
    : associatedIssuers;
  const addButton = (
    <RequireScope scope="mcp:write" resourceId={projectId} level="component">
      <Button variant="secondary" size="md" onClick={onAdd}>
        <Button.LeftIcon>
          <Plus className="size-4" />
        </Button.LeftIcon>
        <Button.Text>Add authorization server</Button.Text>
      </Button>
    </RequireScope>
  );

  const loadError = (
    <FieldError>
      Failed to load the authorization servers. Refresh the page to try again.
    </FieldError>
  );

  let providerControls: ReactNode;
  if (guided) {
    providerControls = (
      <div className="space-y-3">
        {members}
        {isLoading ? null : (
          <>
            {others.length > 0 ? (
              <div className="space-y-3">
                <Text muted small className="block">
                  Also linked, not used by a member:
                </Text>
                {others.map((issuer) => (
                  <RemoteIdentityProviderRow
                    key={issuer.id}
                    issuer={issuer}
                    projectId={projectId}
                    onEdit={() => onEdit(issuer)}
                    onDelete={() => onDelete(issuer)}
                    readOnly={readOnly}
                  />
                ))}
              </div>
            ) : null}
            {isError && loadError}
          </>
        )}
      </div>
    );
  } else if (isLoading) {
    providerControls = (
      <Skeleton label="Loading authorization servers" className="h-9 w-48" />
    );
  } else if (associatedIssuers.length === 0 && isError) {
    providerControls = loadError;
  } else if (associatedIssuers.length === 0) {
    // The button is the empty state: "None yet." beside it says nothing the
    // absent list does not already say.
    providerControls = readOnly ? (
      <Text muted small>
        No authorization servers.
      </Text>
    ) : (
      addButton
    );
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
            readOnly={readOnly}
          />
        ))}
        {isError && loadError}
        {allowAdditionalProviders && !readOnly && addButton}
      </div>
    );
  }

  return (
    <AuthRow
      id={AUTHORIZATION_SERVERS_FIELD_ID}
      label="Authorization servers"
      hint={
        <>
          {guided
            ? "Members that sign people in through their own service, such as Linear. Link each one so signing in to this gateway also signs people in to it."
            : "Where each user signs in to the upstream service. Speakeasy keeps their token and sends it with their requests."}
          <ExplainerDialog title="Authorization servers">
            <Text muted small className="block">
              An authorization server issues the OAuth tokens that let this
              server act for each user in the upstream service — their Linear
              workspace, their Ashby account. Each user signs in there once, and
              Speakeasy keeps that authorization alongside their session.
            </Text>
            <Text muted small className="block">
              The upstream service decides what each person may do, so every
              user acts with their own permissions, not a shared credential.
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
  readOnly,
}: {
  issuer: RemoteSessionIssuer;
  projectId: string;
  readOnly: boolean;
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
        <IssuerLogo
          logoAssetId={issuer.logoAssetId}
          // The name IssuerLink shows beside it.
          name={issuerDisplayName(issuer)}
          size="md"
        />
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
        {!readOnly ? (
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
        ) : null}
      </div>
    </div>
  );
}
