import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import { useCreateOrganizationRemoteSessionIssuerMutation } from "@gram/client/react-query/createOrganizationRemoteSessionIssuer.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { invalidateAllOrganizationRemoteSessionIssuers } from "@gram/client/react-query/organizationRemoteSessionIssuers.js";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  EndpointsFields,
  IssuerUrlField,
} from "../mcp/x/tabs/settings/sections/authentication/IssuerFormFields";
import {
  deriveNameFromUrl,
  deriveSlugFromUrl,
} from "../mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
import { useIssuerDiscovery } from "../mcp/x/tabs/settings/sections/authentication/useIssuerDiscovery";

// Sentinel for the "no project" (organizational) selection. Radix Select treats
// the empty string specially, so we use an explicit value and map it back to an
// omitted projectId on submit.
const ORGANIZATIONAL = "organizational";

// CreateRemoteIdentityProviderSheet is a pared-down sibling of the MCP
// Authentication tab's AttachRemoteIdentityProviderSheet: it configures only the
// upstream issuer (URL, slug, display name, endpoints + RFC 8414 discovery) and
// creates a remote_session_issuer via the org-admin createIssuer endpoint.
// No project selection creates an organizational issuer (project_id NULL,
// inherited everywhere); selecting a project creates a project-specific one. It
// does not register a client or touch an MCP server — client management lives on
// the issuer detail page. On success it routes to the new issuer's detail page.
export function CreateRemoteIdentityProviderSheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const organization = useOrganization();

  const { data: projectsData } = useListProjects({
    organizationId: organization.id,
  });
  const projects = useMemo(() => projectsData?.projects ?? [], [projectsData]);

  const [projectId, setProjectId] = useState<string>(ORGANIZATIONAL);

  // Display name + slug auto-derive from the Issuer URL hostname until the
  // operator edits them, after which the *Dirty flags lock in their value.
  const [name, setName] = useState("");
  const [nameDirty, setNameDirty] = useState(false);
  const [slug, setSlug] = useState("");
  const [slugDirty, setSlugDirty] = useState(false);

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
  } = useIssuerDiscovery(null);

  const createMutation = useCreateOrganizationRemoteSessionIssuerMutation({
    onSuccess: async (created) => {
      await invalidateAllOrganizationRemoteSessionIssuers(queryClient, {
        refetchType: "all",
      });
      toast.success("Remote identity provider created");
      onOpenChange(false);
      orgRoutes.remoteIdentityProviders.issuerDetail.goTo(created.id);
    },
    onError: (error) => {
      // useMutation surfaces error.message via createMutation.error (shown in
      // the inline Alert); a console line keeps the stack for debugging.
      console.error("Create remote identity provider failed", error);
    },
  });

  const submitting = createMutation.isPending;
  const submitError = createMutation.error
    ? createMutation.error instanceof Error && createMutation.error.message
      ? createMutation.error.message
      : "An unexpected error occurred. Please try again."
    : null;
  const { reset: resetCreateMutation } = createMutation;

  // Reset transient state whenever the sheet is reopened so a prior draft never
  // leaks into a new creation.
  useEffect(() => {
    if (!open) return;
    setProjectId(ORGANIZATIONAL);
    setName("");
    setNameDirty(false);
    setSlug("");
    setSlugDirty(false);
    setIssuerUrl("");
    resetEndpointState();
    clearDiscoverError();
    resetCreateMutation();
  }, [
    open,
    setIssuerUrl,
    resetEndpointState,
    clearDiscoverError,
    resetCreateMutation,
  ]);

  const submittable = useMemo(
    () => !!slug.trim() && !!issuerUrl.trim(),
    [slug, issuerUrl],
  );

  const handleSubmit = () => {
    if (!submittable || submitting) return;
    createMutation.mutate({
      request: {
        createIssuerRequestBody: {
          projectId: projectId === ORGANIZATIONAL ? undefined : projectId,
          slug: slug.trim(),
          issuer: issuerUrl.trim(),
          name: name.trim() || undefined,
          authorizationEndpoint: authorizationEndpoint.trim() || undefined,
          tokenEndpoint: tokenEndpoint.trim() || undefined,
          registrationEndpoint: registrationEndpoint.trim() || undefined,
          jwksUri: jwksUri.trim() || undefined,
          // RFC 8414 metadata arrays are NOT NULL server-side. Forward what
          // discovery returned, or empty arrays when the operator typed
          // everything by hand.
          scopesSupported: discoveredSnapshot?.scopesSupported ?? [],
          grantTypesSupported: discoveredSnapshot?.grantTypesSupported ?? [],
          responseTypesSupported:
            discoveredSnapshot?.responseTypesSupported ?? [],
          tokenEndpointAuthMethodsSupported:
            discoveredSnapshot?.tokenEndpointAuthMethodsSupported ?? [],
          // CIMD support is parsed during discovery and persisted here so the
          // issuer can offer the CIMD client type. Defaults false when the
          // operator skipped Discover and typed the endpoints by hand.
          clientIdMetadataDocumentSupported:
            discoveredSnapshot?.clientIdMetadataDocumentSupported ?? false,
        },
      },
    });
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            New Remote Identity Provider
          </SheetTitle>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          <Stack gap={4}>
            <Stack gap={2}>
              <Label className="text-muted-foreground text-xs">Scope</Label>
              <Select value={projectId} onValueChange={setProjectId}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ORGANIZATIONAL}>
                    Organizational (all projects)
                  </SelectItem>
                  {projects.map((project) => (
                    <SelectItem key={project.id} value={project.id}>
                      {project.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Type muted small>
                Organizational providers are inherited by every project. Choose
                a project to scope the provider to it.
              </Type>
            </Stack>

            <IssuerUrlField
              issuerUrl={issuerUrl}
              onIssuerUrlChange={(value) => {
                setIssuerUrl(value);
                clearDiscoverError();
                if (!slugDirty) {
                  const derived = deriveSlugFromUrl(value);
                  if (derived) setSlug(derived);
                }
                if (!nameDirty) {
                  const derivedName = deriveNameFromUrl(value);
                  if (derivedName) setName(derivedName);
                }
                // When the URL diverges from a settled discovery the endpoints
                // are stale; reset them so the operator re-runs Discover.
                if (
                  discoveredSnapshot &&
                  value.trim() !== discoveredSnapshot.url
                ) {
                  resetEndpointState();
                }
              }}
            />

            <Stack gap={2}>
              <Label className="text-muted-foreground text-xs">Slug</Label>
              <Input
                value={slug}
                onChange={(value) => {
                  setSlug(value);
                  setSlugDirty(true);
                }}
                placeholder="my-identity-provider"
              />
              <Type muted small>
                Identifier for this identity provider. Auto-derived from the
                Issuer URL until you edit it.
              </Type>
            </Stack>

            <Stack gap={2}>
              <Label className="text-muted-foreground text-xs">
                Display name (optional)
              </Label>
              <Input
                value={name}
                onChange={(value) => {
                  setName(value);
                  setNameDirty(true);
                }}
                placeholder="My Identity Provider"
              />
              <Type muted small>
                Friendly label shown in the dashboard. Auto-derived from the
                Issuer URL until you edit it; falls back to the Issuer URL when
                left blank.
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
          </Stack>

          {submitError && (
            <Alert variant="error" dismissible={false}>
              {submitError}
            </Alert>
          )}
        </div>

        <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
          <Button
            variant="secondary"
            disabled={submitting}
            onClick={() => onOpenChange(false)}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            disabled={!submittable || submitting}
            onClick={handleSubmit}
          >
            <Button.Text>{submitting ? "Creating…" : "Create"}</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
