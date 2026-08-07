import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { invalidateAllGlobalRemoteSessionIssuer } from "@gram/client/react-query/globalRemoteSessionIssuer.js";
import { invalidateAllGlobalRemoteSessionIssuers } from "@gram/client/react-query/globalRemoteSessionIssuers.js";
import { useRefreshGlobalRemoteSessionIssuerMetadataMutation } from "@gram/client/react-query/refreshGlobalRemoteSessionIssuerMetadata.js";
import { useUpdateGlobalRemoteSessionIssuerMutation } from "@gram/client/react-query/updateGlobalRemoteSessionIssuer.js";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import {
  EndpointsFields,
  IssuerUrlField,
} from "../mcp/x/tabs/settings/sections/authentication/IssuerFormFields";
import { useIssuerDiscovery } from "../mcp/x/tabs/settings/sections/authentication/useIssuerDiscovery";
import { issuerDisplayName } from "../remote-identity-providers/issuerDisplay";
import {
  SettingsField,
  SettingsSection,
} from "../remote-identity-providers/issuerSettingsFields";
import { buildUpdateIssuerForm } from "../remote-identity-providers/issuerSettingsForm";
import { DeletePlatformIssuerDialog } from "./DeletePlatformIssuerDialog";

// PlatformSettingsTab is the catalog's edit surface. It mirrors the tenant
// issuer Settings tab but talks to the adminRemoteSessions endpoints
// throughout, so no component here has to decide at runtime which tier it is
// operating on.
export function PlatformSettingsTab({
  issuer,
  globalClientCount,
  tenantClientCount,
}: {
  issuer: RemoteSessionIssuer;
  globalClientCount: number;
  tenantClientCount: number;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [name, setName] = useState(issuer.name ?? "");
  const [slug, setSlug] = useState(issuer.slug);
  const [clientSetupDocumentationUrl, setClientSetupDocumentationUrl] =
    useState(issuer.clientSetupDocumentationUrl ?? "");
  const [showDelete, setShowDelete] = useState(false);

  const {
    issuerUrl,
    setIssuerUrl,
    authorizationEndpoint,
    setAuthorizationEndpoint,
    tokenEndpoint,
    setTokenEndpoint,
    registrationEndpoint,
    setRegistrationEndpoint,
    jwksUri,
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
  } = useIssuerDiscovery(
    {
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
      clientIdMetadataDocumentSupported:
        issuer.clientIdMetadataDocumentSupported,
      serviceDocumentation: issuer.serviceDocumentation ?? "",
      opPolicyUri: issuer.opPolicyUri ?? "",
      opTosUri: issuer.opTosUri ?? "",
    },
    // Seed the saved values into the fields but not a discovery snapshot, so
    // Discover stays available against the existing issuer URL.
    { seedSnapshot: false, scope: "platform" },
  );

  const update = useUpdateGlobalRemoteSessionIssuerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGlobalRemoteSessionIssuer(queryClient, {
          refetchType: "all",
        }),
        invalidateAllGlobalRemoteSessionIssuers(queryClient, {
          refetchType: "all",
        }),
      ]);
      toast.success("Platform provider updated");
    },
    onError: (error) => {
      console.error("Update platform identity provider failed", error);
    },
  });

  // Same split as the tenant tab: while the URL field still matches what is
  // saved, Refresh is the better of the two (one click, only RFC 8414-derived
  // columns, and it refuses a document naming a different authorization
  // server). Once the field diverges Refresh would rediscover the old URL and
  // the server would abort on the mismatch, so Discover-then-Save takes over.
  const issuerUrlMatchesSaved = issuerUrl.trim() === issuer.issuer;

  const refreshMetadata = useRefreshGlobalRemoteSessionIssuerMetadataMutation({
    onSuccess: async (result) => {
      // The tab is keyed on the issuer id and the discovery hook seeds its
      // fields on mount only, so invalidating alone would leave these inputs
      // showing the pre-refresh endpoints. Push the discovered values in
      // directly, leaving unsaved name/slug edits intact.
      setAuthorizationEndpoint(result.issuer.authorizationEndpoint ?? "");
      setTokenEndpoint(result.issuer.tokenEndpoint ?? "");
      setRegistrationEndpoint(result.issuer.registrationEndpoint ?? "");
      setJwksUri(result.issuer.jwksUri ?? "");

      await Promise.all([
        invalidateAllGlobalRemoteSessionIssuer(queryClient, {
          refetchType: "all",
        }),
        invalidateAllGlobalRemoteSessionIssuers(queryClient, {
          refetchType: "all",
        }),
      ]);

      if (result.discoveryWarnings.length > 0) {
        toast.warning("Refreshed with warnings", {
          description: result.discoveryWarnings.join(" "),
        });
        return;
      }
      toast.success("Discoverable metadata refreshed");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to refresh metadata",
      );
    },
  });

  const saveError = update.error
    ? update.error instanceof Error && update.error.message
      ? update.error.message
      : "An unexpected error occurred. Please try again."
    : null;

  const adoptedByTenants = tenantClientCount > 0;

  const handleSave = () => {
    update.mutate({
      request: {
        updateRemoteSessionIssuerForm: buildUpdateIssuerForm({
          id: issuer.id,
          name,
          slug,
          clientSetupDocumentationUrl,
          issuerUrl,
          authorizationEndpoint,
          tokenEndpoint,
          registrationEndpoint,
          jwksUri,
          discoveredSnapshot,
        }),
      },
    });
  };

  return (
    <div className="flex max-w-2xl flex-col gap-6">
      {/* Editing a tenant-adopted issuer repoints live OAuth configuration for
          organizations that never asked for the change, and the endpoints are
          the part that breaks their flows rather than merely relabelling them.
          There is no server-side guard on this, so say so plainly here. */}
      {adoptedByTenants && (
        <Alert variant="warning" dismissible={false}>
          {tenantClientCount}{" "}
          {tenantClientCount === 1
            ? "tenant-owned client is"
            : "tenant-owned clients are"}{" "}
          registered with this provider. Changing the issuer URL or endpoints
          affects those organizations immediately.
        </Alert>
      )}

      <SettingsSection
        title="Provider"
        description="How this identity provider is labelled in every organization's dashboard."
      >
        <SettingsField label="Display name" value={name} onChange={setName} />
        <SettingsField label="Slug" value={slug} onChange={setSlug} />
      </SettingsSection>

      <SettingsSection
        title="Issuer configuration"
        description="The upstream Authorization Server. Refresh to re-read its RFC 8414 metadata, or change the issuer URL and run discovery to point this provider somewhere else."
      >
        <IssuerUrlField
          issuerUrl={issuerUrl}
          onIssuerUrlChange={(value) => {
            setIssuerUrl(value);
            clearDiscoverError();
            // The endpoint fields belong to the settled URL — the saved issuer,
            // or the last discovery if one ran. Once the typed URL diverges
            // they describe the wrong provider, and Save would submit the old
            // provider's endpoints under the new URL. Clear them so the
            // operator re-runs Discover or types values for the new provider.
            const settledUrl = discoveredSnapshot?.url ?? issuer.issuer;
            if (value.trim() !== settledUrl) {
              resetEndpointState();
            }
          }}
        />
        <EndpointsFields
          issuerUrl={issuerUrl}
          authorizationEndpoint={authorizationEndpoint}
          tokenEndpoint={tokenEndpoint}
          registrationEndpoint={registrationEndpoint}
          jwksUri={jwksUri}
          endpointWarnings={endpointWarnings}
          discoverPending={discoverPending}
          discoverError={discoverError}
          // Discover only earns its place once the URL has been edited; until
          // then Refresh Discoverable Metadata below does the same job better.
          showDiscoverControls={showDiscoverControls && !issuerUrlMatchesSaved}
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
        {issuerUrlMatchesSaved && (
          <div className="flex flex-col gap-1.5">
            <div>
              <Button
                variant="secondary"
                onClick={() =>
                  refreshMetadata.mutate({
                    request: { riskIDRequestBody: { id: issuer.id } },
                  })
                }
                // Also blocked while Save runs: a save built from pre-refresh
                // fields finishing after the refresh would silently undo it.
                disabled={refreshMetadata.isPending || update.isPending}
              >
                <Button.Text>
                  {refreshMetadata.isPending
                    ? "Refreshing…"
                    : "Refresh Discoverable Metadata"}
                </Button.Text>
              </Button>
            </div>
            <Text small muted>
              Re-reads this provider's RFC 8414 metadata and saves the endpoints
              and advertised capabilities immediately. Your other changes on
              this page are not saved.
            </Text>
          </div>
        )}
      </SettingsSection>

      <SettingsSection
        title="Client setup"
        description="Documentation linked from the New Client sheet so operators in every organization can set up an OAuth client with this provider themselves."
      >
        <SettingsField
          label="Client setup documentation URL"
          value={clientSetupDocumentationUrl}
          onChange={setClientSetupDocumentationUrl}
        />
      </SettingsSection>

      {saveError && (
        <Alert variant="error" dismissible={false}>
          {saveError}
        </Alert>
      )}

      <div>
        <Button
          onClick={handleSave}
          disabled={update.isPending || refreshMetadata.isPending}
        >
          <Button.Text>
            {update.isPending ? "Saving…" : "Save changes"}
          </Button.Text>
        </Button>
      </div>

      <div className="border-destructive/30 flex flex-col gap-2 border p-4">
        <Text className="font-medium">Danger Zone</Text>
        <Text small muted>
          Removes this provider from the platform catalog for every
          organization. Refused while any client is still registered with it.
        </Text>
        <div>
          <Button
            variant="destructive-primary"
            onClick={() => setShowDelete(true)}
          >
            <Button.Text>Delete provider</Button.Text>
          </Button>
        </div>
      </div>

      {showDelete && (
        <DeletePlatformIssuerDialog
          issuerId={issuer.id}
          issuerLabel={issuerDisplayName(issuer)}
          globalClientCount={globalClientCount}
          tenantClientCount={tenantClientCount}
          onClose={() => setShowDelete(false)}
          onDeleted={() => orgRoutes.platformRemoteIdentityProviders.goTo()}
        />
      )}
    </div>
  );
}
