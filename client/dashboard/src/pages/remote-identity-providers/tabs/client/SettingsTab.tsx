import { RequireScope } from "@/components/require-scope";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import {
  UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat,
  type UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat as AuthAudienceFormat,
} from "@gram/client/models/components/updateremotesessionclientform.js";
import { invalidateAllOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { useOrganizationRemoteSessionIssuer } from "@gram/client/react-query/organizationRemoteSessionIssuer.js";
import { useUpdateOrganizationRemoteSessionClientMutation } from "@gram/client/react-query/updateOrganizationRemoteSessionClient.js";
import { Button } from "@/components/ui/Button";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { remoteSessionClientDisplayName } from "../../clientDisplay";
import {
  ClientAssertionAudienceField,
  TokenEndpointAuthMethodField,
} from "../../../mcp/x/tabs/settings/sections/authentication/IssuerFormFields";
import {
  clientSecretUpdateValue,
  isPrivateKeyJwtAuthMethod,
  narrowTokenEndpointAuthMethod,
  parseScopes,
} from "../../../mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
import { IssuerScopeOverrideAlert } from "../../clientAlerts";
import { DeleteClientDialog, RotateClientDialog } from "../../clientDialogs";
import { KeySetField } from "./KeySetField";

export function SettingsTab({
  client,
  issuerId,
}: {
  client: RemoteSessionClient;
  issuerId: string;
}): JSX.Element {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const persistedAuthMethod =
    narrowTokenEndpointAuthMethod(client.tokenEndpointAuthMethod, true) ?? "";
  const [authMethod, setAuthMethod] = useState<
    CreateRemoteSessionClientFormTokenEndpointAuthMethod | ""
  >(persistedAuthMethod);
  const [authAudienceFormat, setAuthAudienceFormat] = useState<
    AuthAudienceFormat | undefined
  >(client.tokenEndpointAuthAudienceFormat);
  const [scope, setScope] = useState((client.scope ?? []).join(", "));
  const [audience, setAudience] = useState(client.audience ?? "");
  const [clientSecret, setClientSecret] = useState("");
  const [showDelete, setShowDelete] = useState(false);
  const [showRotate, setShowRotate] = useState(false);
  // A rotation re-registers the client at the issuer's published registration
  // endpoint, so the issuer decides whether one is possible at all.
  const { data: issuer } = useOrganizationRemoteSessionIssuer({ id: issuerId });
  const issuerRegistrationEndpoint = issuer?.registrationEndpoint ?? null;
  // The key set link saves on its own endpoint the moment it changes, while
  // these fields wait for Save. Selecting a set and immediately saving
  // private_key_jwt would otherwise race: the update can reach the server first
  // and be refused for having no set attached. AIM-156 makes that method
  // selectable; the sequencing is here so it is already right when it does.
  const [keySetPending, setKeySetPending] = useState(false);
  const privateKeyJwtSelected = isPrivateKeyJwtAuthMethod(authMethod);
  const privateKeyJwtMissingKeySet =
    privateKeyJwtSelected && client.jsonWebKeySetId == null;

  // The key-set field saves independently. If a detach succeeds while
  // private_key_jwt is only a local, unsaved selection, its refetch removes the
  // prerequisite beneath this draft. Reconcile to the persisted method so the
  // next Save cannot submit a combination the server must reject. This also
  // handles the set disappearing in another tab.
  useEffect(() => {
    if (client.jsonWebKeySetId != null) return;

    setAuthMethod((current) => {
      if (
        !isPrivateKeyJwtAuthMethod(current) ||
        isPrivateKeyJwtAuthMethod(persistedAuthMethod)
      ) {
        return current;
      }
      return persistedAuthMethod;
    });
  }, [client.jsonWebKeySetId, persistedAuthMethod]);

  const handleAuthMethodChange = (
    method: CreateRemoteSessionClientFormTokenEndpointAuthMethod | "",
  ) => {
    setAuthMethod(method);
    if (isPrivateKeyJwtAuthMethod(method)) setClientSecret("");
  };

  const update = useUpdateOrganizationRemoteSessionClientMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionClient(queryClient, {
        refetchType: "all",
      });
      setClientSecret("");
      toast.success("Client updated");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to update client",
      );
    },
  });

  const handleSave = () => {
    update.mutate({
      request: {
        updateRemoteSessionClientForm: {
          id: client.id,
          tokenEndpointAuthMethod: authMethod || undefined,
          tokenEndpointAuthAudienceFormat: privateKeyJwtSelected
            ? authAudienceFormat
            : undefined,
          scope: parseScopes(scope),
          audience: audience.trim() || undefined,
          clientSecret: clientSecretUpdateValue(authMethod, clientSecret),
        },
      },
    });
  };

  return (
    <div className="flex max-w-2xl flex-col gap-6">
      <div className="flex flex-col gap-4">
        <TokenEndpointAuthMethodField
          value={authMethod}
          onChange={handleAuthMethodChange}
          allowPrivateKeyJwt={client.jsonWebKeySetId != null}
        />
        {privateKeyJwtSelected && (
          <ClientAssertionAudienceField
            value={
              authAudienceFormat ??
              UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat.Issuer
            }
            onChange={setAuthAudienceFormat}
          />
        )}
        {/* org:admin like the Save button below: attach and detach are
            org:admin on the server, so a reader must not get a live control
            whose every change 403s. */}
        <RequireScope scope="org:admin" level="component">
          <KeySetField
            client={client}
            issuerId={issuerId}
            onPendingChange={setKeySetPending}
          />
        </RequireScope>
        <div className="flex flex-col gap-1.5">
          <Label>Scopes (comma-separated)</Label>
          <Input value={scope} onChange={setScope} />
          <IssuerScopeOverrideAlert
            issuerId={issuerId}
            scopeOverride={issuer?.scopeOverride}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label>Audience</Label>
          <Input value={audience} onChange={setAudience} />
        </div>
        {privateKeyJwtSelected ? (
          <Text small muted>
            Any existing client secret is retained but not used with
            private_key_jwt.
          </Text>
        ) : (
          <div className="flex flex-col gap-1.5">
            <Label>Rotate client secret</Label>
            <Input
              type="password"
              value={clientSecret}
              onChange={setClientSecret}
              placeholder="Enter a new secret to rotate; leave blank to keep current"
            />
            <Text small muted>
              The secret is encrypted at rest and never displayed. Leave blank
              to keep the existing secret.
            </Text>
          </div>
        )}
        <div>
          <RequireScope scope="org:admin" level="component">
            <Button
              onClick={handleSave}
              disabled={
                update.isPending || keySetPending || privateKeyJwtMissingKeySet
              }
            >
              <Button.Text>
                {update.isPending ? "Saving…" : "Save changes"}
              </Button.Text>
            </Button>
          </RequireScope>
        </div>
      </div>

      <div className="flex flex-col gap-2 border p-4">
        <Text className="font-medium">Registration</Text>
        <RegistrationStatus
          client={client}
          issuerRegistrationEndpoint={issuerRegistrationEndpoint}
        />
        {isRotatableRegistration(client) && issuerRegistrationEndpoint && (
          <div>
            <RequireScope scope="org:admin" level="component">
              <Button variant="secondary" onClick={() => setShowRotate(true)}>
                <Button.Text>Rotate client</Button.Text>
              </Button>
            </RequireScope>
          </div>
        )}
      </div>

      <div className="border-destructive/30 flex flex-col gap-2 border p-4">
        <Text className="font-medium">Danger Zone</Text>
        <Text small muted>
          Deleting this client is permanent and revokes all of its sessions.
        </Text>
        <div>
          <RequireScope scope="org:admin" level="component">
            <Button
              variant="destructive-primary"
              onClick={() => setShowDelete(true)}
            >
              <Button.Text>Delete client</Button.Text>
            </Button>
          </RequireScope>
        </div>
      </div>

      {showRotate && (
        <RotateClientDialog
          clientId={client.id}
          clientLabel={remoteSessionClientDisplayName(client)}
          onClose={() => setShowRotate(false)}
        />
      )}

      {showDelete && (
        <DeleteClientDialog
          clientId={client.id}
          clientLabel={remoteSessionClientDisplayName(client)}
          onClose={() => setShowDelete(false)}
          onDeleted={() =>
            routes.remoteIdentityProviders.issuerDetail.goTo(issuerId)
          }
        />
      )}
    </div>
  );
}

// RegistrationStatus explains what a rotation would do for this client: whether
// the identity provider publishes a registration endpoint Gram can re-register
// it at, whether the provider has already stopped recognizing it, and when its
// secret expires.
function RegistrationStatus({
  client,
  issuerRegistrationEndpoint,
}: {
  client: RemoteSessionClient;
  issuerRegistrationEndpoint: string | null;
}): JSX.Element {
  if (!isRotatableRegistration(client)) {
    return (
      <Text small muted>
        {client.clientIdMetadataUri
          ? "This client uses a client ID metadata document hosted by Gram. It is never registered with the identity provider, so it cannot expire and has nothing to rotate."
          : "This client authenticates with a signed assertion bound to a key set, which dynamic registration cannot reproduce. Manage its key set instead of rotating it."}
      </Text>
    );
  }
  if (client.upstreamRejectedAt) {
    return (
      <Text small className="text-destructive">
        The identity provider stopped recognizing this client on{" "}
        {client.upstreamRejectedAt.toLocaleString()}.{" "}
        {issuerRegistrationEndpoint
          ? "Rotate it to register a replacement; users reconnect once afterwards."
          : "The provider publishes no registration endpoint, so replace the client ID and secret by hand."}
      </Text>
    );
  }
  const expiryNote = client.clientSecretExpiresAt
    ? ` The secret expires ${client.clientSecretExpiresAt.toLocaleString()}.`
    : "";
  if (issuerRegistrationEndpoint) {
    return (
      <Text small muted>
        Gram re-registers this client at {issuerRegistrationEndpoint} if the
        identity provider stops recognizing it or its secret expires; rotate now
        to replace it ahead of time.{expiryNote}
      </Text>
    );
  }
  return (
    <Text small muted>
      The identity provider publishes no registration endpoint, so Gram cannot
      re-register this client; replace its credentials by hand if the provider
      stops recognizing them.{expiryNote}
    </Text>
  );
}

// isRotatableRegistration mirrors the server's rule: a rotation replaces a
// dynamically registered client_id and secret, which a CIMD client (its
// client_id is a document URL) and a private_key_jwt client (bound to a key
// set) do not have.
function isRotatableRegistration(client: RemoteSessionClient): boolean {
  // The SDK enum does not list private_key_jwt until it becomes selectable,
  // but the server already stores and refuses to rotate it.
  const authMethod: string | undefined = client.tokenEndpointAuthMethod;
  return !client.clientIdMetadataUri && authMethod !== "private_key_jwt";
}
