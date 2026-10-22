import { isKnownEnumValue } from "@/lib/enum";
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
import {
  ClientKeyRegistration,
  ScopeMultiSelect,
  useCustomerManagedKeys,
} from "@/lib/remote-identity";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import {
  UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat,
  type UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat as AuthAudienceFormat,
} from "@gram/client/models/components/updateremotesessionclientform.js";
import { invalidateAllOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { invalidateAllOrganizationRemoteSessionClients } from "@gram/client/react-query/organizationRemoteSessionClients.js";
import { invalidateAllOrganizationRemoteSessionClientSessions } from "@gram/client/react-query/organizationRemoteSessionClientSessions.js";
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
import { ChangeClientIdDialog } from "../../clientDialogs";
import { InfoField, InfoText } from "../../detailFields";
import { formatTimestamp } from "./formatTimestamp";
import { KeySetField } from "./KeySetField";

export function ClientCard({
  client,
  issuer,
  issuerId,
  onDirtyChange,
}: {
  client: RemoteSessionClient;
  issuer: RemoteSessionIssuer | undefined;
  issuerId: string;
  // Called with whether the card holds edits that Save has not stored, so a
  // container can warn before discarding them.
  onDirtyChange?: (dirty: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  // The organization update endpoint requires org:admin; readers get the same
  // fields disabled.
  const { hasAnyScope } = useRBAC();
  const supportsAudience =
    client.tokenEndpointAuthAudienceFormat === undefined ||
    isKnownEnumValue(
      UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat,
      client.tokenEndpointAuthAudienceFormat,
    );
  const supportsAuthMethod =
    client.tokenEndpointAuthMethod === undefined ||
    narrowTokenEndpointAuthMethod(client.tokenEndpointAuthMethod, true) !==
      undefined;
  const supportsEditing =
    supportsAuthMethod &&
    (client.tokenEndpointAuthMethod !== "private_key_jwt" || supportsAudience);
  const canEdit = hasAnyScope(["org:admin"]) && supportsEditing;
  const persistedAuthMethod =
    narrowTokenEndpointAuthMethod(client.tokenEndpointAuthMethod, true) ?? "";
  const [authMethod, setAuthMethod] = useState<
    CreateRemoteSessionClientFormTokenEndpointAuthMethod | ""
  >(persistedAuthMethod);
  const [authAudienceFormat, setAuthAudienceFormat] = useState<
    AuthAudienceFormat | undefined
  >(
    isKnownEnumValue(
      UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat,
      client.tokenEndpointAuthAudienceFormat,
    )
      ? client.tokenEndpointAuthAudienceFormat
      : undefined,
  );
  const [displayName, setDisplayName] = useState(client.displayName ?? "");
  const [clientIdDraft, setClientIdDraft] = useState(client.clientId);
  const [confirmClientIdChange, setConfirmClientIdChange] = useState(false);
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
  const shownDisplayName = canEdit ? displayName : (client.displayName ?? "");
  const shownClientId = canEdit ? clientIdDraft : client.clientId;
  // A CIMD client's ID is the metadata document URL Speakeasy generated, so
  // only manually entered and dynamically registered IDs can be corrected.
  const clientIdEditable = !client.clientIdMetadataUri;
  const trimmedClientId = clientIdDraft.trim();
  const clientIdChanged =
    clientIdEditable && trimmedClientId !== client.clientId;
  const shownScope = canEdit ? scope : (client.scope ?? []);
  const shownAudience = canEdit ? audience : (client.audience ?? "");
  const privateKeyJwtSelected = isPrivateKeyJwtAuthMethod(shownAuthMethod);
  const serviceAccount = client.credentialOwner === "self";
  const { entitled: customerManagedKeys } = useCustomerManagedKeys();
  const issuerName = issuer?.name ?? "the authorization server";
  let privateKeyJwtUnavailable: string | undefined;
  if (!supportsAudience) {
    privateKeyJwtUnavailable =
      "Refresh the page to use this authentication method";
  } else if (serviceAccount) {
    privateKeyJwtUnavailable = customerManagedKeys
      ? "attach a key set first"
      : "Requires KMS";
  }
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
    if (isPrivateKeyJwtAuthMethod(method) && !supportsAudience) return;
    setAuthMethod(method);
    if (isPrivateKeyJwtAuthMethod(method)) setClientSecret("");
  };

  const update = useUpdateOrganizationRemoteSessionClientMutation({
    onSuccess: async (saved) => {
      await Promise.all([
        invalidateAllOrganizationRemoteSessionClient(queryClient, {
          refetchType: "all",
        }),
        // Lists and breadcrumbs show the name, and a changed client ID
        // revokes the client's sessions.
        invalidateAllOrganizationRemoteSessionClients(queryClient, {
          refetchType: "all",
        }),
        invalidateAllOrganizationRemoteSessionClientSessions(queryClient, {
          refetchType: "all",
        }),
      ]);
      // An emptied audience is omitted, so show what was actually kept.
      setAudience(saved.audience ?? "");
      setDisplayName(saved.displayName ?? "");
      setClientIdDraft(saved.clientId);
      setConfirmClientIdChange(false);
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

  // Unchanged scopes are omitted, so a save never turns an unset scope into [].
  const savedScope = client.scope ?? [];
  const scopeChanged =
    scope.length !== savedScope.length ||
    scope.some((s, i) => s !== savedScope[i]);

  const dirty =
    canEdit &&
    (displayName.trim() !== (client.displayName ?? "") ||
      clientIdChanged ||
      authMethod !== persistedAuthMethod ||
      scopeChanged ||
      audience !== (client.audience ?? "") ||
      clientSecret !== "" ||
      (privateKeyJwtSelected &&
        authAudienceFormat !== client.tokenEndpointAuthAudienceFormat) ||
      (legacyCallbackMode !== undefined &&
        legacyCallbackMode !== client.legacyCallbackUrl));
  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const submit = () => {
    if (!canEdit || (privateKeyJwtSelected && !supportsAudience)) return;
    const trimmedDisplayName = displayName.trim();
    update.mutate({
      request: {
        updateRemoteSessionClientForm: {
          id: client.id,
          clientId: clientIdChanged ? trimmedClientId : undefined,
          // An empty string clears the name; unchanged names are omitted.
          displayName:
            trimmedDisplayName !== (client.displayName ?? "")
              ? trimmedDisplayName
              : undefined,
          tokenEndpointAuthMethod: authMethod || undefined,
          tokenEndpointAuthAudienceFormat: privateKeyJwtSelected
            ? authAudienceFormat
            : undefined,
          scope: scopeChanged ? scope : undefined,
          audience: audience.trim() || undefined,
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

  const handleSave = () => {
    if (clientIdChanged) {
      setConfirmClientIdChange(true);
      return;
    }
    submit();
  };

  return (
    <Card>
      <Card.Header>Client</Card.Header>
      <Card.Content>
        {!supportsEditing && (
          <Text small muted>
            Refresh the page before editing this unsupported client
            configuration.
          </Text>
        )}
        <div className="flex flex-col gap-4">
          <div className="grid items-start gap-x-8 gap-y-4 sm:grid-cols-2">
            {!clientIdEditable && (
              <InfoField label="Client ID">
                <InfoText mono>{client.clientId}</InfoText>
              </InfoField>
            )}
            <InfoField label="Client issued at">
              <InfoText>{formatTimestamp(client.clientIdIssuedAt)}</InfoText>
            </InfoField>
            {serviceAccount && (
              <div className="sm:col-span-2">
                <InfoField label="Signs in as">
                  <InfoText>
                    Itself. Every caller shares the one token this client gets
                    from {issuerName}. Set when the client was created.
                  </InfoText>
                </InfoField>
              </div>
            )}
          </div>

          <fieldset
            disabled={!canEdit}
            className="m-0 flex min-w-0 flex-col gap-4 border-0 p-0"
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="client-display-name">Name</Label>
              <Input
                id="client-display-name"
                value={shownDisplayName}
                onChange={setDisplayName}
                placeholder={client.clientId}
                maxLength={200}
                disabled={!canEdit}
              />
              <Text small muted>
                Shown in lists and page titles. Leave empty to show the client
                ID.
              </Text>
            </div>
            {clientIdEditable && (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="client-id">Client ID</Label>
                <Input
                  id="client-id"
                  className="font-mono"
                  value={shownClientId}
                  onChange={setClientIdDraft}
                  disabled={!canEdit}
                />
                <Text small muted>
                  The ID the authorization server issued for this client.
                  Changing it signs out everyone connected through the client.
                </Text>
              </div>
            )}
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
                placeholder="Discovered at sign-in"
                disabled={!canEdit}
              />
              <Text small muted>
                Leave empty to request what the MCP server asks for at each
                sign-in. Choose from the scopes this authorization server
                advertises, or type one to add it.
              </Text>
            </div>
            <TokenEndpointAuthMethodField
              value={shownAuthMethod}
              onChange={handleAuthMethodChange}
              allowPrivateKeyJwt={
                client.jsonWebKeySetId != null && supportsAudience
              }
              hideNone={serviceAccount}
              privateKeyJwtUnavailable={privateKeyJwtUnavailable}
              disabled={!canEdit}
              unsupported={!supportsAuthMethod}
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
            {serviceAccount &&
              isPrivateKeyJwtAuthMethod(persistedAuthMethod) &&
              client.jsonWebKeySetId != null && (
                <ClientKeyRegistration
                  clientRowId={client.id}
                  keySetId={client.jsonWebKeySetId}
                />
              )}
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
                  On for apps registered with the authorization server under the
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
                  privateKeyJwtMissingKeySet ||
                  (clientIdEditable && trimmedClientId === "")
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
      {confirmClientIdChange && (
        <ChangeClientIdDialog
          clientId={client.id}
          newClientId={trimmedClientId}
          isPending={update.isPending}
          onConfirm={submit}
          onClose={() => setConfirmClientIdChange(false)}
        />
      )}
    </Card>
  );
}
