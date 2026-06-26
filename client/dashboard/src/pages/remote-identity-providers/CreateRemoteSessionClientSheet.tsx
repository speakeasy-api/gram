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
import { useFetcher } from "@/contexts/Fetcher";
import { proxyRegisterUpstreamClient } from "@/lib/proxyRegisterUpstreamClient";
import type { RemoteSessionIssuer } from "@gram/client/models/components";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components";
import {
  invalidateAllOrganizationRemoteSessionClients,
  useCreateOrganizationRemoteSessionClientMutation,
  useListProjects,
} from "@gram/client/react-query/index.js";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import {
  ClientCredentialsFields,
  OverridesFields,
  TokenEndpointAuthMethodField,
} from "../mcp/x/tabs/settings/sections/authentication/IssuerFormFields";
import {
  narrowTokenEndpointAuthMethod,
  parseScopes,
} from "../mcp/x/tabs/settings/sections/authentication/issuerFormUtils";

// Sentinel for the unselected project in the org-level-issuer scope picker.
// Radix Select treats the empty string specially, so submission is gated until
// the operator picks a real project (a client must be project-scoped).
const UNSELECTED_PROJECT = "";

// ClientType selects how the new client's credentials are obtained. It is a
// UI-only choice (never sent to the backend, which just persists whatever
// credentials result). Client ID Metadata Document (CIMD) support will add a
// third type here, expanding the form further.
type ClientType = "dcr" | "manual";

const CLIENT_TYPE_LABELS: Record<ClientType, string> = {
  dcr: "Dynamic Client Registration (DCR)",
  manual: "Manual",
};

// CreateRemoteSessionClientSheet registers a standalone remote_session_client
// under the page's already-selected issuer via the org-admin createClient
// endpoint, with no user_session_issuer attachments. It is a pared-down sibling
// of the MCP Authentication tab's AttachRemoteIdentityProviderSheet: the issuer
// is fixed, so it skips the issuer-resolution half and reuses only the
// credentials half (DCR-vs-manual, token endpoint auth method, scope/audience
// overrides). A project-specific issuer's client inherits the issuer's project;
// an organization-level issuer has no project to inherit, so the operator must
// downscope the client to a project in the organization.
export function CreateRemoteSessionClientSheet({
  open,
  onOpenChange,
  issuer,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const { fetch: authedFetch } = useFetcher();

  // DCR is offered as a client type only when the issuer advertises a
  // registration_endpoint (RFC 7591); otherwise the operator pastes a client_id
  // and optional client_secret obtained out-of-band (Manual).
  const registrationEndpoint = issuer.registrationEndpoint?.trim() ?? "";
  const dcrAvailable = registrationEndpoint.length > 0;

  // The selectable client types for this issuer. DCR appears only when the
  // issuer supports it; Manual is always available. When only one type is
  // available the selector is hidden (there is nothing to choose).
  const availableClientTypes = useMemo<ClientType[]>(
    () => (dcrAvailable ? ["dcr", "manual"] : ["manual"]),
    [dcrAvailable],
  );

  // Organization-level issuers (no owning project) have no project to inherit,
  // so the operator must name one; project-specific issuers inherit silently.
  const isOrganizational = !issuer.projectId;

  const { data: projectsData } = useListProjects(
    { organizationId: organization.id },
    undefined,
    { enabled: isOrganizational },
  );
  const projects = useMemo(() => projectsData?.projects ?? [], [projectsData]);

  const [projectId, setProjectId] = useState<string>(UNSELECTED_PROJECT);
  const [clientType, setClientType] = useState<ClientType>(
    dcrAvailable ? "dcr" : "manual",
  );
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [tokenEndpointAuthMethod, setTokenEndpointAuthMethod] = useState<
    CreateRemoteSessionClientFormTokenEndpointAuthMethod | ""
  >("");
  const [scopeOverride, setScopeOverride] = useState("");
  const [audienceOverride, setAudienceOverride] = useState("");

  // DCR may hand back a token_endpoint_auth_method the SDK enum doesn't model;
  // we keep the client record without it but warn the operator after success.
  const unsupportedDcrAuthMethod = useRef<string | null>(null);
  // The proxy-register call is a pre-step the create mutation can't see; track
  // its pending/error state separately and fold both into the submit surface.
  const [registering, setRegistering] = useState(false);
  const [registerError, setRegisterError] = useState<string | null>(null);

  const createMutation = useCreateOrganizationRemoteSessionClientMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionClients(queryClient, {
        refetchType: "all",
      });
      toast.success("Client created");
      if (unsupportedDcrAuthMethod.current) {
        toast.warning(
          `The issuer registered the client with an unsupported token endpoint auth method (${unsupportedDcrAuthMethod.current}); it was not stored on the client.`,
        );
      }
      onOpenChange(false);
    },
    onError: (error) => {
      // useMutation surfaces error.message via createMutation.error (shown in
      // the inline Alert); a console line keeps the stack for debugging.
      console.error("Create remote session client failed", error);
    },
  });

  const submitting = registering || createMutation.isPending;
  const mutationError = createMutation.error
    ? createMutation.error instanceof Error && createMutation.error.message
      ? createMutation.error.message
      : "An unexpected error occurred. Please try again."
    : null;
  const submitError = registerError ?? mutationError;
  const { reset: resetCreateMutation } = createMutation;

  // Reset transient state whenever the sheet is reopened so a prior draft never
  // leaks into a new creation.
  useEffect(() => {
    if (!open) return;
    setProjectId(UNSELECTED_PROJECT);
    setClientType(dcrAvailable ? "dcr" : "manual");
    setClientId("");
    setClientSecret("");
    setTokenEndpointAuthMethod("");
    setScopeOverride("");
    setAudienceOverride("");
    setRegisterError(null);
    unsupportedDcrAuthMethod.current = null;
    resetCreateMutation();
  }, [open, dcrAvailable, resetCreateMutation]);

  const projectChosen = !isOrganizational || projectId !== UNSELECTED_PROJECT;
  // Manual requires a client_id; DCR mints one during proxy registration.
  const credentialsReady =
    clientType === "manual" ? clientId.trim().length > 0 : true;
  const submittable = projectChosen && credentialsReady;

  const handleSubmit = async () => {
    if (!submittable || submitting) return;

    setRegisterError(null);
    unsupportedDcrAuthMethod.current = null;
    resetCreateMutation();

    const parsedScopes = parseScopes(scopeOverride);
    const trimmedAudience = audienceOverride.trim();

    // Obtain client credentials. The DCR client type proxies a registration to
    // the upstream issuer for a fresh client_id / client_secret; Manual uses
    // what the operator typed.
    let credentials: {
      clientId: string;
      clientSecret?: string;
      tokenEndpointAuthMethod?: CreateRemoteSessionClientFormTokenEndpointAuthMethod;
    };
    if (clientType === "dcr") {
      setRegistering(true);
      try {
        const registered = await proxyRegisterUpstreamClient(authedFetch, {
          registrationEndpoint,
          // RFC 7591 §2: scope is a space-separated string at registration time.
          scope: parsedScopes.length > 0 ? parsedScopes.join(" ") : undefined,
          tokenEndpointAuthMethod: tokenEndpointAuthMethod || undefined,
        });
        const narrowedDcrMethod = narrowTokenEndpointAuthMethod(
          registered.tokenEndpointAuthMethod,
        );
        if (registered.tokenEndpointAuthMethod && !narrowedDcrMethod) {
          unsupportedDcrAuthMethod.current = registered.tokenEndpointAuthMethod;
        }
        credentials = {
          clientId: registered.clientId,
          clientSecret: registered.clientSecret || undefined,
          tokenEndpointAuthMethod:
            narrowedDcrMethod ?? (tokenEndpointAuthMethod || undefined),
        };
      } catch (error) {
        setRegisterError(
          error instanceof Error && error.message
            ? error.message
            : "Dynamic Client Registration failed. Please try again.",
        );
        return;
      } finally {
        setRegistering(false);
      }
    } else {
      credentials = {
        clientId: clientId.trim(),
        clientSecret: clientSecret.trim() || undefined,
        tokenEndpointAuthMethod: tokenEndpointAuthMethod || undefined,
      };
    }

    createMutation.mutate({
      request: {
        createOrganizationRemoteSessionClientForm: {
          remoteSessionIssuerId: issuer.id,
          // Project-specific issuers inherit their project (omit); org-level
          // issuers are downscoped to the chosen project.
          projectId: isOrganizational ? projectId : undefined,
          clientId: credentials.clientId,
          clientSecret: credentials.clientSecret,
          tokenEndpointAuthMethod: credentials.tokenEndpointAuthMethod,
          scope: parsedScopes.length > 0 ? parsedScopes : undefined,
          audience: trimmedAudience || undefined,
        },
      },
    });
  };

  let credentialsSection: JSX.Element;
  switch (clientType) {
    case "dcr":
      // DCR mints the client_id/client_secret at save time, so the only input
      // is the token endpoint auth method forwarded to the registration call.
      // The explanatory copy lives under the Client Type selector.
      credentialsSection = (
        <TokenEndpointAuthMethodField
          value={tokenEndpointAuthMethod}
          onChange={setTokenEndpointAuthMethod}
        />
      );
      break;
    // CIMD will add a "cimd" case here; the exhaustiveness check enforces it.
    case "manual":
      // The "OAuth Client Credentials" heading is moved under the Client Type
      // selector (see clientTypeHelp), so the fields render without it.
      credentialsSection = (
        <ClientCredentialsFields
          showHeading={false}
          clientId={clientId}
          clientSecret={clientSecret}
          tokenEndpointAuthMethod={tokenEndpointAuthMethod}
          onClientIdChange={setClientId}
          onClientSecretChange={setClientSecret}
          onTokenEndpointAuthMethodChange={setTokenEndpointAuthMethod}
        />
      );
  }

  // Help text shown under the Client Type selector. It explains the active
  // type and, in Manual mode, points at DCR when the issuer supports it.
  let clientTypeHelp: string;
  switch (clientType) {
    case "dcr":
      clientTypeHelp =
        "The issuer advertises a registration endpoint (RFC 7591), so the platform can automatically register a client on save. You can also choose to manually define an existing client.";
      break;
    case "manual":
      clientTypeHelp =
        "The platform acts as an OAuth client against the upstream issuer. Register a client with the issuer out-of-band and paste the credentials here.";
      if (dcrAvailable) {
        clientTypeHelp +=
          " You can also choose Dynamic Client Registration (DCR) to register a client automatically.";
      }
      break;
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">New Client</SheetTitle>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          <Stack gap={4}>
            {isOrganizational ? (
              <Stack gap={2}>
                <Label className="text-muted-foreground text-xs">Project</Label>
                <Select value={projectId} onValueChange={setProjectId}>
                  <SelectTrigger>
                    <SelectValue placeholder="Select a project" />
                  </SelectTrigger>
                  <SelectContent>
                    {projects.map((project) => (
                      <SelectItem key={project.id} value={project.id}>
                        {project.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Type muted small>
                  This provider is organizational, so the client must be scoped
                  to a project in the organization.
                </Type>
              </Stack>
            ) : (
              <Type muted small>
                The client will be created in this provider's project.
              </Type>
            )}

            <Stack gap={2}>
              {availableClientTypes.length > 1 && (
                <>
                  <Label className="text-muted-foreground text-xs">
                    Client Type
                  </Label>
                  <Select
                    value={clientType}
                    onValueChange={(value) =>
                      setClientType(value as ClientType)
                    }
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {availableClientTypes.map((type) => (
                        <SelectItem key={type} value={type}>
                          {CLIENT_TYPE_LABELS[type]}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </>
              )}
              <Type muted small>
                {clientTypeHelp}
              </Type>
            </Stack>

            {credentialsSection}

            <OverridesFields
              scopeOverride={scopeOverride}
              audienceOverride={audienceOverride}
              onScopeOverrideChange={setScopeOverride}
              onAudienceOverrideChange={setAudienceOverride}
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
            onClick={() => void handleSubmit()}
          >
            <Button.Text>{submitting ? "Creating…" : "Create"}</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
