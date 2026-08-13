import { RequireScope } from "@/components/require-scope";
import { useRBAC } from "@/hooks/useRBAC";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { invalidateAllOrganizationRemoteSessionIssuer } from "@gram/client/react-query/organizationRemoteSessionIssuer.js";
import { invalidateAllOrganizationRemoteSessionIssuers } from "@gram/client/react-query/organizationRemoteSessionIssuers.js";
import { useRefreshOrganizationRemoteSessionIssuerMetadataMutation } from "@gram/client/react-query/refreshOrganizationRemoteSessionIssuerMetadata.js";
import { useUpdateOrganizationRemoteSessionIssuerMutation } from "@gram/client/react-query/updateOrganizationRemoteSessionIssuer.js";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Link } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import {
  EndpointsFields,
  IssuerUrlField,
} from "../../../mcp/x/tabs/settings/sections/authentication/IssuerFormFields";
import { useIssuerDiscovery } from "../../../mcp/x/tabs/settings/sections/authentication/useIssuerDiscovery";
import { IssuerDuplicateWarning } from "../../../mcp/x/tabs/settings/sections/authentication/IssuerDuplicateWarning";
import { useIssuerDuplicatePreflight } from "../../../mcp/x/tabs/settings/sections/authentication/useIssuerDuplicatePreflight";
import { DeleteIssuerDialog } from "../../RemoteIdentityProviders";
import { issuerDisplayName } from "../../issuerDisplay";
import { SettingsField, SettingsSection } from "../../issuerSettingsFields";
import { buildUpdateIssuerForm } from "../../issuerSettingsForm";

export function SettingsTab({
  issuer,
}: {
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [name, setName] = useState(issuer.name ?? "");
  const [slug, setSlug] = useState(issuer.slug);
  const [clientSetupDocumentationUrl, setClientSetupDocumentationUrl] =
    useState(issuer.clientSetupDocumentationUrl ?? "");
  const [showDelete, setShowDelete] = useState(false);
  const { hasAnyScope } = useRBAC();
  const hasOrgAdminScope = hasAnyScope(["org:admin"]);

  // Issuer URL + endpoints + RFC 8414 discovery live in the shared hook, seeded
  // from the saved issuer so Discover/Reset work against the current values.
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
      revocationEndpoint: issuer.revocationEndpoint ?? "",
      serviceDocumentation: issuer.serviceDocumentation ?? "",
      opPolicyUri: issuer.opPolicyUri ?? "",
      opTosUri: issuer.opTosUri ?? "",
    },
    // Seed the saved values into the fields but not a discovery snapshot, so the
    // Discover control is available against the existing issuer URL. This tab
    // edits organization-level issuers too, which have no project to authorize
    // against, so metadata is fetched through the org-scoped endpoint.
    { seedSnapshot: false, scope: "organization" },
  );

  const update = useUpdateOrganizationRemoteSessionIssuerMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionIssuer(queryClient, {
        refetchType: "all",
      });
      toast.success("Provider updated");
    },
    onError: (error) => {
      console.error("Update remote identity provider failed", error);
    },
  });

  // Which of the two rediscovery controls applies comes down to which URL would
  // be discovered against. Refresh sends the issuer id and the server reads the
  // stored URL; Discover sends whatever is currently typed. So while the field
  // still matches what is saved they would do the same thing, and Refresh is
  // the better of the two: one click, only RFC 8414-derived columns, audited,
  // and it refuses a document that names a different authorization server.
  //
  // Once the field diverges, Refresh is not merely worse but wrong — it would
  // discover the old URL, and the server would abort on the mismatch anyway.
  // Repointing a provider is exactly what Discover-then-Save is for, so the two
  // swap rather than sit side by side.
  const issuerUrlMatchesSaved = issuerUrl.trim() === issuer.issuer;

  // Repointing a provider can duplicate an existing one just as creating it
  // can, so the same preflight runs here. It is gated on the URL having
  // diverged from what is saved: while they match, the only record it could
  // report is this one. excludeId covers the remaining case, a
  // normalization-equivalent edit (a trailing slash on your own URL) that the
  // shared candidate set still matches.
  const [settledIssuerUrl, setSettledIssuerUrl] = useState(issuer.issuer);
  const { matches: duplicateMatches } = useIssuerDuplicatePreflight({
    issuerUrl: settledIssuerUrl,
    scope: "organization",
    // Gated on settledIssuerUrl, NOT on issuerUrlMatchesSaved: that flag tracks
    // the live input, and gating on it while keying the query on the settled
    // value lets the two disagree — type a new URL, blur, then type the saved
    // one back without blurring, and the gate closes while the key still points
    // at the other URL.
    enabled: settledIssuerUrl.trim() !== issuer.issuer,
    excludeId: issuer.id,
  });

  const refreshMetadata =
    useRefreshOrganizationRemoteSessionIssuerMetadataMutation({
      onSuccess: async (result) => {
        // The tab is keyed on the issuer id, which never changes, and the
        // discovery hook seeds its fields on mount only. Invalidating alone
        // would refresh the cache while leaving these inputs showing the old
        // endpoints, so the discovered values are pushed in directly. Only
        // these four are touched, leaving unsaved name/slug edits intact.
        setAuthorizationEndpoint(result.issuer.authorizationEndpoint ?? "");
        setTokenEndpoint(result.issuer.tokenEndpoint ?? "");
        setRegistrationEndpoint(result.issuer.registrationEndpoint ?? "");
        setJwksUri(result.issuer.jwksUri ?? "");

        await Promise.all([
          invalidateAllOrganizationRemoteSessionIssuer(queryClient, {
            refetchType: "all",
          }),
          invalidateAllOrganizationRemoteSessionIssuers(queryClient, {
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
      <SettingsSection
        title="Provider"
        description="How this identity provider is labelled in the dashboard."
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
          onIssuerUrlSettled={setSettledIssuerUrl}
          duplicateWarning={
            <IssuerDuplicateWarning
              viewerScope="organization"
              matches={duplicateMatches}
              renderLink={(match) => (
                <Button asChild variant="secondary">
                  <Link
                    to={orgRoutes.remoteIdentityProviders.issuerDetail.href(
                      match.id,
                    )}
                  >
                    View existing provider
                  </Link>
                </Button>
              )}
            />
          }
          onIssuerUrlChange={(value) => {
            setIssuerUrl(value);
            // Any edit invalidates the last blur, so a warning cannot outlive
            // the URL it describes.
            setSettledIssuerUrl("");
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
          // Two gates. Discovery here goes through the org-scoped endpoint,
          // which requires org:admin, but this page admits org:read — hide the
          // control rather than leave a button that can only produce a
          // permission error, matching how Save and Delete below are gated.
          // And Discover only earns its place once the URL has been edited;
          // until then Refresh Discoverable Metadata below does the same job
          // better.
          showDiscoverControls={
            showDiscoverControls && hasOrgAdminScope && !issuerUrlMatchesSaved
          }
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
        {hasOrgAdminScope && issuerUrlMatchesSaved && (
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
        description="Documentation linked from the New Client sheet so operators can set up an OAuth client with this provider themselves."
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
        <RequireScope scope="org:admin" level="component">
          <Button
            onClick={handleSave}
            disabled={update.isPending || refreshMetadata.isPending}
          >
            <Button.Text>
              {update.isPending ? "Saving…" : "Save changes"}
            </Button.Text>
          </Button>
        </RequireScope>
      </div>

      <div className="border-destructive/30 flex flex-col gap-2 border p-4">
        <Text className="font-medium">Danger Zone</Text>
        <Text small muted>
          Deleting this provider is permanent. All clients must be deleted
          first.
        </Text>
        <div>
          <RequireScope scope="org:admin" level="component">
            <Button
              variant="destructive-primary"
              onClick={() => setShowDelete(true)}
            >
              <Button.Text>Delete provider</Button.Text>
            </Button>
          </RequireScope>
        </div>
      </div>

      {showDelete && (
        <DeleteIssuerDialog
          issuerId={issuer.id}
          issuerLabel={issuerDisplayName(issuer)}
          onClose={() => setShowDelete(false)}
          onDeleted={() => orgRoutes.remoteIdentityProviders.goTo()}
        />
      )}
    </div>
  );
}
