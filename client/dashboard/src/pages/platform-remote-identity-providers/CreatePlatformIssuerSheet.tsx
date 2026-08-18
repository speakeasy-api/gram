import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import { useCreateGlobalRemoteSessionIssuerMutation } from "@gram/client/react-query/createGlobalRemoteSessionIssuer.js";
import { invalidateAllGlobalRemoteSessionIssuers } from "@gram/client/react-query/globalRemoteSessionIssuers.js";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Link } from "react-router";
import { Stack } from "@/components/ui/Stack";
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
import { IssuerDuplicateWarning } from "../mcp/x/tabs/settings/sections/authentication/IssuerDuplicateWarning";
import { useIssuerDuplicatePreflight } from "../mcp/x/tabs/settings/sections/authentication/useIssuerDuplicatePreflight";
import { buildCreateIssuerForm } from "../remote-identity-providers/issuerSettingsForm";

// CreatePlatformIssuerSheet is the catalog's counterpart to the tenant
// CreateRemoteIdentityProviderSheet. It has no scope selector: everything
// created here is global by construction, which is also why the tenant sheet
// gained no "Platform" option — a platform tier sitting one click away in a
// dropdown a customer admin uses daily is a mis-click that publishes their
// issuer to every organization.
export function CreatePlatformIssuerSheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();

  // Display name + slug auto-derive from the Issuer URL hostname until the
  // operator edits them, after which the *Dirty flags lock in their value.
  const [name, setName] = useState("");
  const [nameDirty, setNameDirty] = useState(false);
  const [slug, setSlug] = useState("");
  const [slugDirty, setSlugDirty] = useState(false);
  const [clientSetupDocumentationUrl, setClientSetupDocumentationUrl] =
    useState("");

  // The Issuer URL as it stood when the operator last left the field. Held
  // separately from the live input so the duplicate preflight runs on a settled
  // value rather than once per keystroke.
  const [settledIssuerUrl, setSettledIssuerUrl] = useState("");
  // Platform scope: the catalog only. The global tier is unique on slug but not
  // on issuer, so nothing but this warning will catch a second catalog entry for
  // one authorization server. Tenant records naming the same URL are a separate
  // question, answered by the convergence page.
  const { matches: duplicateMatches } = useIssuerDuplicatePreflight({
    issuerUrl: settledIssuerUrl,
    scope: "platform",
    enabled: open,
  });

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
    // A global issuer belongs to no organization and no project, so discovery
    // goes through the platform-admin endpoint rather than authorizing against
    // whichever tenant the admin happens to be viewing.
  } = useIssuerDiscovery(null, { scope: "platform" });

  const createMutation = useCreateGlobalRemoteSessionIssuerMutation({
    onSuccess: async (created) => {
      await invalidateAllGlobalRemoteSessionIssuers(queryClient, {
        refetchType: "all",
      });
      toast.success("Platform identity provider created");
      onOpenChange(false);
      orgRoutes.platformRemoteIdentityProviders.issuerDetail.goTo(created.id);
    },
    onError: (error) => {
      console.error("Create platform identity provider failed", error);
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
    setName("");
    setNameDirty(false);
    setSlug("");
    setSlugDirty(false);
    setClientSetupDocumentationUrl("");
    setIssuerUrl("");
    setSettledIssuerUrl("");
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
        createRemoteSessionIssuerForm: buildCreateIssuerForm({
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
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            New Platform Remote Identity Provider
          </SheetTitle>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          <Stack gap={4}>
            <Alert variant="info" dismissible={false}>
              This provider will be visible to every organization on the
              platform.
            </Alert>

            <IssuerUrlField
              issuerUrl={issuerUrl}
              onIssuerUrlSettled={setSettledIssuerUrl}
              duplicateWarning={
                <IssuerDuplicateWarning
                  viewerScope="platform"
                  matches={duplicateMatches}
                  renderLink={(match) => (
                    <Button asChild variant="secondary">
                      <Link
                        to={orgRoutes.platformRemoteIdentityProviders.issuerDetail.href(
                          match.id,
                        )}
                        onClick={() => onOpenChange(false)}
                      >
                        View existing provider
                      </Link>
                    </Button>
                  )}
                />
              }
              onIssuerUrlChange={(value) => {
                setIssuerUrl(value);
                // Any edit invalidates the last blur, so a warning cannot
                // outlive the URL it describes.
                setSettledIssuerUrl("");
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
              <Text muted small>
                Identifier for this identity provider. Auto-derived from the
                Issuer URL until you edit it.
              </Text>
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
              <Text muted small>
                Friendly label shown in every organization's dashboard. Falls
                back to the Issuer URL when left blank.
              </Text>
            </Stack>

            <Stack gap={2}>
              <Label className="text-muted-foreground text-xs">
                Client setup documentation URL (optional)
              </Label>
              <Input
                value={clientSetupDocumentationUrl}
                onChange={setClientSetupDocumentationUrl}
                placeholder="https://docs.example.com/oauth/apps"
              />
              <Text muted small>
                Linked from the New Client sheet so operators in every
                organization can set up an OAuth client with this provider
                themselves.
              </Text>
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
