import { Label } from "@/components/ui/label";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { Stack } from "@/components/ui/stack";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Input } from "@/components/ui/input";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import {
  ClientCredentialsFields,
  EndpointsFields,
  IssuerUrlField,
  OverridesFields,
} from "./IssuerFormFields";
import { parseScopes } from "./issuerFormUtils";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";
import { useIssuerDiscovery } from "./useIssuerDiscovery";

export function ModifyRemoteIdentityProviderSheet({
  open,
  onOpenChange,
  userSessionIssuer,
  issuer,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  userSessionIssuer: UserSessionIssuer;
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  // Fetch every remote_session_client tied to this issuer (across all
  // user_session_issuers in the project), walking every page. We need the
  // unfiltered list to compute which OTHER MCP servers are also using this
  // issuer so the operator gets a heads-up that issuer/endpoint edits will
  // affect them — a single-page fetch would silently undercount once a
  // project crosses the default page size of 50.
  const { items: allClients, isLoading: isLoadingClients } =
    useAllRemoteSessionClients(
      { remoteSessionIssuerId: issuer.id },
      { enabled: open },
    );
  const primaryClient: RemoteSessionClient | undefined = allClients.find(
    (clientRow) =>
      clientRow.userSessionIssuerIds.includes(userSessionIssuer.id),
  );
  const otherUserSessionIssuerIds = useMemo(
    () =>
      new Set(
        allClients
          .flatMap((clientRow) => clientRow.userSessionIssuerIds)
          .filter((id) => id !== userSessionIssuer.id),
      ),
    [allClients, userSessionIssuer.id],
  );

  // Look up the MCP servers in the project that bind any of those "other"
  // user_session_issuers — those are the servers whose authentication will
  // be touched by issuer-level edits in this sheet.
  const { data: serversResult } = useMcpServers(undefined, undefined, {
    enabled: open && otherUserSessionIssuerIds.size > 0,
  });
  const affectedMcpServers = useMemo(() => {
    const servers = serversResult?.mcpServers ?? [];
    return servers.filter(
      (mcpServer) =>
        mcpServer.userSessionIssuerId != null &&
        otherUserSessionIssuerIds.has(mcpServer.userSessionIssuerId),
    );
  }, [serversResult, otherUserSessionIssuerIds]);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            Modify Remote Identity Provider
          </SheetTitle>
        </SheetHeader>

        {open && (
          <ModifyRemoteIdentityProviderSheetBody
            issuer={issuer}
            primaryClient={primaryClient}
            isLoadingClient={isLoadingClients}
            affectedMcpServers={affectedMcpServers}
            onClose={() => onOpenChange(false)}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function ModifyRemoteIdentityProviderSheetBody({
  issuer,
  primaryClient,
  isLoadingClient,
  affectedMcpServers,
  onClose,
}: {
  issuer: RemoteSessionIssuer;
  primaryClient: RemoteSessionClient | undefined;
  isLoadingClient: boolean;
  affectedMcpServers: { id: string; name?: string | undefined }[];
  onClose: () => void;
}) {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  // Issuer URL + endpoints + discovery state come from the shared hook. The
  // loaded record seeds the snapshot so the Discover/Reset slot and the
  // URL-change reset behave the same way the Attach sheet does, just with
  // saved data as the baseline.
  const discovery = useIssuerDiscovery({
    issuerUrl: issuer.issuer,
    authorizationEndpoint: issuer.authorizationEndpoint ?? "",
    tokenEndpoint: issuer.tokenEndpoint ?? "",
    registrationEndpoint: issuer.registrationEndpoint ?? "",
    jwksUri: issuer.jwksUri ?? "",
    scopesSupported: issuer.scopesSupported ?? [],
    grantTypesSupported: issuer.grantTypesSupported ?? [],
    responseTypesSupported: issuer.responseTypesSupported ?? [],
    tokenEndpointAuthMethodsSupported:
      issuer.tokenEndpointAuthMethodsSupported ?? [],
    clientIdMetadataDocumentSupported: issuer.clientIdMetadataDocumentSupported,
    serviceDocumentation: issuer.serviceDocumentation ?? "",
    opPolicyUri: issuer.opPolicyUri ?? "",
    opTosUri: issuer.opTosUri ?? "",
  });
  const {
    issuerUrl,
    setIssuerUrl,
    authorizationEndpoint,
    tokenEndpoint,
    registrationEndpoint,
    jwksUri,
    setAuthorizationEndpoint,
    setTokenEndpoint,
    setRegistrationEndpoint,
    setJwksUri,
    discoveredSnapshot,
    discoverPending,
    discoverError,
    clearDiscoverError,
    runDiscover,
    handleResetEndpoints,
    resetEndpointState,
    showDiscoverControls,
    showResetControls,
    endpointWarnings,
  } = discovery;

  // Editable display name, seeded from the saved record. The body remounts on
  // each open (the `{open && <Body />}` pattern), so seeding from props here is
  // fresh per open. Submitting an empty string clears the name to NULL on the
  // backend.
  const [name, setName] = useState(issuer.name ?? "");

  // Client-side form state. clientId is informational only — the API has no
  // rotate path, so it stays read-only. clientSecret starts blank; a typed
  // value rotates the secret, blank means "leave unchanged".
  const [clientSecret, setClientSecret] = useState("");
  const [tokenEndpointAuthMethod, setTokenEndpointAuthMethod] = useState<
    CreateRemoteSessionClientFormTokenEndpointAuthMethod | ""
  >("");
  const [scopeOverride, setScopeOverride] = useState("");
  const [audienceOverride, setAudienceOverride] = useState("");

  const modifyMutation = useMutation({
    mutationFn: async () => {
      if (!primaryClient) {
        throw new Error("No primary remote session client to update.");
      }
      const parsedScopes = parseScopes(scopeOverride);
      const trimmedAudience = audienceOverride.trim();

      // Update the remote_session_issuer. The backend now reads an explicit
      // empty string on the four nullable endpoint fields as "clear to
      // NULL"; an omitted field keeps the existing value. Send the trimmed
      // input directly (including "") so blanking out a field in the UI
      // actually clears the saved record — especially registration_endpoint,
      // which is the signal Gram uses for "DCR is supported on this issuer".
      await client.remoteSessionIssuers.update({
        updateRemoteSessionIssuerForm: {
          id: issuer.id,
          issuer: issuerUrl.trim(),
          // Empty string clears the saved display name to NULL, same three-state
          // semantics the backend applies to the nullable endpoint fields.
          name: name.trim(),
          authorizationEndpoint: authorizationEndpoint.trim(),
          tokenEndpoint: tokenEndpoint.trim(),
          registrationEndpoint: registrationEndpoint.trim(),
          jwksUri: jwksUri.trim(),
          scopesSupported: discoveredSnapshot?.scopesSupported,
          grantTypesSupported: discoveredSnapshot?.grantTypesSupported,
          responseTypesSupported: discoveredSnapshot?.responseTypesSupported,
          tokenEndpointAuthMethodsSupported:
            discoveredSnapshot?.tokenEndpointAuthMethodsSupported,
          // undefined when no snapshot — the server COALESCEs to keep the
          // stored CIMD-support value; a fresh discovery overwrites it.
          clientIdMetadataDocumentSupported:
            discoveredSnapshot?.clientIdMetadataDocumentSupported,
          // Discovery-only, no form inputs. The snapshot is seeded from the saved
          // record, so absent a fresh discovery these round-trip unchanged; a
          // fresh one overwrites them, and "" clears a URL the issuer dropped.
          serviceDocumentation: discoveredSnapshot?.serviceDocumentation,
          opPolicyUri: discoveredSnapshot?.opPolicyUri,
          opTosUri: discoveredSnapshot?.opTosUri,
        },
      });

      // Update the remote_session_client overrides. clientSecret is only
      // forwarded when the operator typed a new value (blank means keep the
      // stored secret in place — see ClientCredentialsFields placeholder).
      await client.remoteSessionClients.update({
        updateRemoteSessionClientForm: {
          id: primaryClient.id,
          clientSecret: clientSecret.trim() || undefined,
          tokenEndpointAuthMethod: tokenEndpointAuthMethod || undefined,
          // Backend update uses COALESCE — omitting (undefined) keeps the
          // stored value, sending an empty array would clear it. Mirror the
          // audience handling here: only send when non-empty so the Modify
          // UI never silently wipes existing overrides. Clearing today
          // requires the operator to detach + re-attach the identity
          // provider with new settings.
          scope: parsedScopes.length > 0 ? parsedScopes : undefined,
          audience: trimmedAudience || undefined,
        },
      });
    },
    onSuccess: async () => {
      await Promise.all([
        invalidateAllRemoteSessionIssuers(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionClients(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Identity provider updated");
      onClose();
    },
    onError: (error) => {
      console.error("Modify identity provider failed", error);
    },
  });

  const submitting = modifyMutation.isPending;
  const submitError = modifyMutation.error
    ? modifyMutation.error instanceof Error && modifyMutation.error.message
      ? modifyMutation.error.message
      : "An unexpected error occurred. Please try again."
    : null;

  // Seed client-derived fields exactly once per sheet open, the first time
  // the client lookup resolves. The sheet body remounts on close (the
  // `{open && <Body />}` pattern in the parent), so the ref resets naturally
  // on the next open — no need to wire it to the open prop. Guarding via a
  // ref instead of a narrow useEffect dependency keeps the linter happy AND
  // prevents subsequent refetches from clobbering in-progress edits.
  const clientFieldsInitializedRef = useRef(false);
  useEffect(() => {
    if (!primaryClient || clientFieldsInitializedRef.current) return;
    setTokenEndpointAuthMethod(primaryClient.tokenEndpointAuthMethod ?? "");
    setScopeOverride((primaryClient.scope ?? []).join(", "));
    setAudienceOverride(primaryClient.audience ?? "");
    clientFieldsInitializedRef.current = true;
  }, [primaryClient]);

  const submittable = !!primaryClient && issuerUrl.trim().length > 0;

  const handleSubmit = () => {
    if (!submittable || submitting || !primaryClient) return;
    modifyMutation.mutate();
  };

  return (
    <>
      <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
        {affectedMcpServers.length > 0 && (
          <Alert variant="warning" dismissible={false}>
            <Stack gap={2}>
              <Type className="font-medium">
                Heads up — this identity provider is shared with{" "}
                {affectedMcpServers.length === 1
                  ? "1 other MCP server"
                  : `${affectedMcpServers.length} other MCP servers`}{" "}
                in this project.
              </Type>
              <Type small>
                Issuer URL and endpoint edits apply to every server that uses
                this provider. Client credentials, scope, and audience changes
                only affect this server.
              </Type>
              <Type small mono>
                {affectedMcpServers
                  .map((server) => server.name?.trim() || server.id.slice(0, 8))
                  .join(", ")}
              </Type>
            </Stack>
          </Alert>
        )}

        <IssuerUrlField
          issuerUrl={issuerUrl}
          onIssuerUrlChange={(value) => {
            setIssuerUrl(value);
            clearDiscoverError();
            // Same reset semantics as Attach: when the URL diverges from the
            // settled state, every downstream field was tied to that prior
            // URL. Clear them so re-Discover (or manual re-entry) produces a
            // coherent result. clientId is intentionally NOT cleared — the
            // existing client record stays in place since the API has no
            // rotate path for client_id.
            if (discoveredSnapshot && value.trim() !== discoveredSnapshot.url) {
              resetEndpointState();
              setClientSecret("");
              setTokenEndpointAuthMethod("");
              setScopeOverride("");
              setAudienceOverride("");
            }
          }}
        />

        <Stack gap={2}>
          <Label className="text-muted-foreground text-xs">Slug</Label>
          <Type small mono>
            {issuer.slug}
          </Type>
          <Type muted small>
            Slug is the stable identifier for this identity provider and can't
            be renamed here.
          </Type>
        </Stack>

        <Stack gap={2}>
          <Label className="text-muted-foreground text-xs">
            Display name (optional)
          </Label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="My Identity Provider"
          />
          <Type muted small>
            Friendly label shown in the dashboard. Clear it to fall back to the
            Issuer URL.
          </Type>
        </Stack>

        <EndpointsFields
          issuerUrl={issuerUrl}
          authorizationEndpoint={authorizationEndpoint}
          tokenEndpoint={tokenEndpoint}
          registrationEndpoint={registrationEndpoint}
          jwksUri={jwksUri}
          endpointWarnings={endpointWarnings}
          discoverPending={discoverPending}
          discoverError={discoverError}
          showDiscoverControls={showDiscoverControls}
          showResetControls={showResetControls}
          onAuthorizationEndpointChange={setAuthorizationEndpoint}
          onTokenEndpointChange={setTokenEndpoint}
          onRegistrationEndpointChange={setRegistrationEndpoint}
          onJwksUriChange={setJwksUri}
          onDiscover={() => {
            runDiscover(issuerUrl);
          }}
          onResetEndpoints={handleResetEndpoints}
        />

        {isLoadingClient ? (
          <Type muted small>
            Loading client credentials…
          </Type>
        ) : (
          <ClientCredentialsFields
            clientId={primaryClient?.clientId ?? ""}
            clientSecret={clientSecret}
            tokenEndpointAuthMethod={tokenEndpointAuthMethod}
            clientIdEditable={false}
            clientSecretLabel="Client Secret (leave blank to keep existing)"
            clientSecretPlaceholder="Type a new secret to rotate"
            onClientIdChange={() => undefined}
            onClientSecretChange={setClientSecret}
            onTokenEndpointAuthMethodChange={setTokenEndpointAuthMethod}
          />
        )}

        <OverridesFields
          scopeOverride={scopeOverride}
          audienceOverride={audienceOverride}
          onScopeOverrideChange={setScopeOverride}
          onAudienceOverrideChange={setAudienceOverride}
        />

        {submitError && (
          <Alert variant="error" dismissible={false}>
            {submitError}
          </Alert>
        )}
      </div>

      <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
        <Button variant="secondary" disabled={submitting} onClick={onClose}>
          <Button.Text>Cancel</Button.Text>
        </Button>
        <Button
          variant="primary"
          disabled={!submittable || submitting || isLoadingClient}
          onClick={handleSubmit}
        >
          <Button.Text>{submitting ? "Saving…" : "Save"}</Button.Text>
        </Button>
      </SheetFooter>
    </>
  );
}
