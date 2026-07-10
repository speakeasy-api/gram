import { RequireScope } from "@/components/require-scope";
import { Card } from "@/components/ui/card";
import { Field, FieldLabel } from "@/components/ui/field";
import { Type } from "@/components/ui/type";
import { useOrgRoutes } from "@/routes";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { invalidateAllOrganizationRemoteSessionIssuer } from "@gram/client/react-query/organizationRemoteSessionIssuer.js";
import { useUpdateOrganizationRemoteSessionIssuerMutation } from "@gram/client/react-query/updateOrganizationRemoteSessionIssuer.js";
import { Alert, Button, Input } from "@/components/ui/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import {
  EndpointsFields,
  IssuerUrlField,
} from "../../../mcp/x/tabs/settings/sections/authentication/IssuerFormFields";
import { useIssuerDiscovery } from "../../../mcp/x/tabs/settings/sections/authentication/useIssuerDiscovery";
import { DeleteIssuerDialog } from "../../RemoteIdentityProviders";
import { issuerDisplayName } from "../../issuerDisplay";

function SettingsSection({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-4 border-b pb-6 last:border-b-0 last:pb-0">
      <div className="flex flex-col gap-1">
        <Type className="font-medium">{title}</Type>
        {description && (
          <Type small muted>
            {description}
          </Type>
        )}
      </div>
      {children}
    </div>
  );
}

export function SettingsTab({
  issuer,
}: {
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [name, setName] = useState(issuer.name ?? "");
  const [slug, setSlug] = useState(issuer.slug);
  const [showDelete, setShowDelete] = useState(false);

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
    },
    // Seed the saved values into the fields but not a discovery snapshot, so the
    // Discover control is available against the existing issuer URL.
    { seedSnapshot: false },
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

  const saveError = update.error
    ? update.error instanceof Error && update.error.message
      ? update.error.message
      : "An unexpected error occurred. Please try again."
    : null;

  const handleSave = () => {
    // Only forward the RFC 8414 metadata arrays when a fresh discovery produced
    // them for the current URL; otherwise omit so the server keeps the existing
    // values (COALESCE narg semantics).
    const arraysFromDiscovery =
      discoveredSnapshot && discoveredSnapshot.url === issuerUrl.trim();
    update.mutate({
      request: {
        updateRemoteSessionIssuerForm: {
          id: issuer.id,
          name: name.trim(),
          slug: slug.trim(),
          issuer: issuerUrl.trim(),
          authorizationEndpoint: authorizationEndpoint.trim(),
          tokenEndpoint: tokenEndpoint.trim(),
          registrationEndpoint: registrationEndpoint.trim(),
          jwksUri: jwksUri.trim(),
          scopesSupported: arraysFromDiscovery
            ? discoveredSnapshot.scopesSupported
            : undefined,
          grantTypesSupported: arraysFromDiscovery
            ? discoveredSnapshot.grantTypesSupported
            : undefined,
          responseTypesSupported: arraysFromDiscovery
            ? discoveredSnapshot.responseTypesSupported
            : undefined,
          tokenEndpointAuthMethodsSupported: arraysFromDiscovery
            ? discoveredSnapshot.tokenEndpointAuthMethodsSupported
            : undefined,
          clientIdMetadataDocumentSupported: arraysFromDiscovery
            ? discoveredSnapshot.clientIdMetadataDocumentSupported
            : undefined,
        },
      },
    });
  };

  return (
    <div className="flex max-w-2xl flex-col gap-6">
      <SettingsSection
        title="Provider"
        description="How this identity provider is labelled in the dashboard."
      >
        <Field>
          <FieldLabel htmlFor="issuer-display-name">Display name</FieldLabel>
          <Input
            id="issuer-display-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="issuer-slug">Slug</FieldLabel>
          <Input
            id="issuer-slug"
            value={slug}
            onChange={(e) => setSlug(e.target.value)}
          />
        </Field>
      </SettingsSection>

      <SettingsSection
        title="Issuer configuration"
        description="The upstream Authorization Server. Run discovery to fill the endpoints from its RFC 8414 metadata."
      >
        <IssuerUrlField
          issuerUrl={issuerUrl}
          onIssuerUrlChange={(value) => {
            setIssuerUrl(value);
            clearDiscoverError();
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
      </SettingsSection>

      {saveError && (
        <Alert variant="error" dismissible={false}>
          {saveError}
        </Alert>
      )}

      <div>
        <RequireScope scope="org:admin" level="component">
          <Button onClick={handleSave} disabled={update.isPending}>
            <Button.Text>
              {update.isPending ? "Saving…" : "Save changes"}
            </Button.Text>
          </Button>
        </RequireScope>
      </div>

      <Card className="border-destructive/30 gap-2">
        <Type className="font-medium">Danger Zone</Type>
        <Type small muted>
          Deleting this provider is permanent. All clients must be deleted
          first.
        </Type>
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
      </Card>

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
