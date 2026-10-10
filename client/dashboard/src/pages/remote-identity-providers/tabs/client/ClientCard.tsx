import { PlatformAdminOnlyPanel } from "@/components/platform-admin-only-panel";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { ScopeMultiSelect } from "@/lib/remote-identity";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import {
  UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat,
  type UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat as AuthAudienceFormat,
} from "@gram/client/models/components/updateremotesessionclientform.js";
import { invalidateAllOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { useUpdateOrganizationRemoteSessionClientMutation } from "@gram/client/react-query/updateOrganizationRemoteSessionClient.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import {
  ClientAssertionAudienceField,
  TokenEndpointAuthMethodField,
} from "../../../mcp/x/tabs/settings/sections/authentication/IssuerFormFields";
import {
  clientSecretUpdateValue,
  isPrivateKeyJwtAuthMethod,
  narrowTokenEndpointAuthMethod,
} from "../../../mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
import { IssuerScopeOverrideAlert } from "../../clientAlerts";
import { InfoField, InfoText } from "../../detailFields";
import { formatTimestamp } from "./formatTimestamp";
import { KeySetField } from "./KeySetField";

export function ClientCard({
  client,
  issuer,
  issuerId,
}: {
  client: RemoteSessionClient;
  issuer: RemoteSessionIssuer | undefined;
  issuerId: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  // The organization update endpoint requires org:admin; readers get the same
  // fields disabled.
  const { hasAnyScope } = useRBAC();
  const canEdit = hasAnyScope(["org:admin"]);
  const persistedAuthMethod =
    narrowTokenEndpointAuthMethod(client.tokenEndpointAuthMethod, true) ?? "";
  const [authMethod, setAuthMethod] = useState<
    CreateRemoteSessionClientFormTokenEndpointAuthMethod | ""
  >(persistedAuthMethod);
  const [authAudienceFormat, setAuthAudienceFormat] = useState<
    AuthAudienceFormat | undefined
  >(client.tokenEndpointAuthAudienceFormat);
  const [scope, setScope] = useState<string[]>(client.scope ?? []);
  const [audience, setAudience] = useState(client.audience ?? "");
  const [clientSecret, setClientSecret] = useState("");
  // Undefined until a platform admin flips the switch, so the switch tracks
  // the saved value (including a Migrate from the warning above) and a save
  // only sends the flag when it was deliberately changed.
  const isPlatformAdmin = useIsPlatformAdmin();
  const [legacyCallbackMode, setLegacyCallbackMode] = useState<
    boolean | undefined
  >(undefined);
  const legacyCallbackChecked = legacyCallbackMode ?? client.legacyCallbackUrl;
  // The key set link saves on its own endpoint the moment it changes, while
  // these fields wait for Save. Selecting a set and immediately saving
  // private_key_jwt would otherwise race: the update can reach the server first
  // and be refused for having no set attached.
  const [keySetPending, setKeySetPending] = useState(false);
  // Readers have no draft, so they see the saved values as they refetch.
  const shownAuthMethod = canEdit ? authMethod : persistedAuthMethod;
  const shownAuthAudienceFormat = canEdit
    ? authAudienceFormat
    : client.tokenEndpointAuthAudienceFormat;
  const shownScope = canEdit ? scope : (client.scope ?? []);
  const shownAudience = canEdit ? audience : (client.audience ?? "");
  const privateKeyJwtSelected = isPrivateKeyJwtAuthMethod(shownAuthMethod);
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
      setLegacyCallbackMode(undefined);
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
          scope,
          audience: audience.trim(),
          clientSecret: clientSecretUpdateValue(authMethod, clientSecret),
          legacyCallbackUrl:
            isPlatformAdmin &&
            legacyCallbackMode !== undefined &&
            legacyCallbackMode !== client.legacyCallbackUrl
              ? legacyCallbackMode
              : undefined,
        },
      },
    });
  };

  return (
    <Card>
      <Card.Header>Client</Card.Header>
      <Card.Content>
        <div className="flex flex-col gap-4">
          <div className="grid items-start gap-x-8 gap-y-4 sm:grid-cols-2">
            <InfoField label="Client ID">
              <InfoText mono>{client.clientId}</InfoText>
            </InfoField>
            <InfoField label="Client issued at">
              <InfoText>{formatTimestamp(client.clientIdIssuedAt)}</InfoText>
            </InfoField>
          </div>

          <fieldset
            disabled={!canEdit}
            className="m-0 flex min-w-0 flex-col gap-4 border-0 p-0"
          >
            <div className="flex flex-col gap-1.5">
              <Label id="client-scopes-label" htmlFor="client-scopes">
                Scopes
              </Label>
              <ScopeMultiSelect
                id="client-scopes"
                labelId="client-scopes-label"
                options={issuer?.scopesSupported ?? []}
                value={shownScope}
                onValueChange={setScope}
                placeholder="No scopes"
                disabled={!canEdit}
              />
              <Text small muted>
                Choose from the scopes this identity provider advertises, or
                type one to add it.
              </Text>
              {/* Only warn once there are client scopes to be overridden. */}
              {shownScope.length > 0 && (
                <IssuerScopeOverrideAlert issuer={issuer} />
              )}
            </div>
            <TokenEndpointAuthMethodField
              value={shownAuthMethod}
              onChange={handleAuthMethodChange}
              allowPrivateKeyJwt={client.jsonWebKeySetId != null}
              disabled={!canEdit}
            />
            {privateKeyJwtSelected && (
              <ClientAssertionAudienceField
                value={
                  shownAuthAudienceFormat ??
                  UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat.Issuer
                }
                onChange={setAuthAudienceFormat}
                disabled={!canEdit}
              />
            )}
            {/* org:admin like Save: attach and detach are org:admin on the
                server, so a reader must not get a live control whose every
                change 403s. */}
            <RequireScope scope="org:admin" level="component">
              <KeySetField
                client={client}
                issuerId={issuerId}
                onPendingChange={setKeySetPending}
                disabled={!canEdit}
              />
            </RequireScope>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="client-audience">Audience</Label>
              <Input
                id="client-audience"
                value={shownAudience}
                onChange={setAudience}
                disabled={!canEdit}
              />
            </div>
            {privateKeyJwtSelected ? (
              <Text small muted>
                Any existing client secret is retained but not used with
                private_key_jwt.
              </Text>
            ) : (
              canEdit && (
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="client-secret">Rotate client secret</Label>
                  <Input
                    id="client-secret"
                    type="password"
                    value={clientSecret}
                    onChange={setClientSecret}
                    placeholder="Enter a new secret to rotate; leave blank to keep current"
                  />
                  <Text small muted>
                    The secret is encrypted at rest and never displayed. Leave
                    blank to keep the existing secret.
                  </Text>
                </div>
              )
            )}
            <PlatformAdminOnlyPanel>
              <div className="flex flex-col gap-1.5">
                <div className="flex items-center gap-3">
                  <Switch
                    aria-labelledby="legacy-callback-mode-label"
                    checked={legacyCallbackChecked}
                    onCheckedChange={setLegacyCallbackMode}
                    disabled={!canEdit}
                  />
                  <Label id="legacy-callback-mode-label">
                    Legacy callback compatibility mode
                  </Label>
                </div>
                <Text small muted>
                  On for apps registered with the identity provider under the
                  legacy /oauth/callback URL. Turn it off once the app has the
                  /mcp/remote_login_callback URL registered. Saved with your
                  other changes.
                </Text>
              </div>
            </PlatformAdminOnlyPanel>
          </fieldset>

          {canEdit && (
            <div>
              <Button
                onClick={handleSave}
                disabled={
                  update.isPending ||
                  keySetPending ||
                  privateKeyJwtMissingKeySet
                }
              >
                <Button.Text>
                  {update.isPending ? "Saving…" : "Save changes"}
                </Button.Text>
              </Button>
            </div>
          )}
        </div>
      </Card.Content>
    </Card>
  );
}
