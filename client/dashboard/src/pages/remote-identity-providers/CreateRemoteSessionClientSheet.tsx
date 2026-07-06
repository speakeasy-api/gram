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
import { useSdkClient } from "@/contexts/Sdk";
import { proxyRegisterUpstreamClient } from "@/lib/proxyRegisterUpstreamClient";
import type { RemoteSessionIssuer } from "@gram/client/models/components";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components";
import {
  invalidateAllOrganizationRemoteSessionClients,
  useListProjects,
} from "@gram/client/react-query/index.js";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  ClientTypeFields,
  OverridesFields,
} from "../mcp/x/tabs/settings/sections/authentication/IssuerFormFields";
import {
  availableClientTypes,
  type ClientType,
  narrowTokenEndpointAuthMethod,
  parseScopes,
} from "../mcp/x/tabs/settings/sections/authentication/issuerFormUtils";

// Sentinel for the unselected project in the org-level-issuer scope picker.
// Radix Select treats the empty string specially, so submission is gated until
// the operator picks a real project (a client must be project-scoped).
const UNSELECTED_PROJECT = "";

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
  const client = useSdkClient();

  // DCR is offered as a client type only when the issuer advertises a
  // registration_endpoint (RFC 7591); otherwise the operator pastes a client_id
  // and optional client_secret obtained out-of-band (Manual).
  const registrationEndpoint = issuer.registrationEndpoint?.trim() ?? "";
  const dcrAvailable = registrationEndpoint.length > 0;

  // CIMD is offered only when the issuer advertises support for Client ID
  // Metadata Documents (parsed during issuer discovery); the platform then
  // hosts the document and uses its URL as the client_id, so no credentials are
  // collected.
  const cimdAvailable = issuer.clientIdMetadataDocumentSupported;

  // The selectable client types for this issuer. DCR and CIMD appear only when
  // the issuer supports them; Manual is always available. The first entry is
  // the default (an automatic type when one is available).
  const clientTypes = useMemo(
    () => availableClientTypes({ dcrAvailable, cimdAvailable }),
    [dcrAvailable, cimdAvailable],
  );
  const defaultClientType = clientTypes[0] ?? "manual";

  // Organization-level issuers (no owning project) have no project to inherit,
  // so the operator must name one; project-specific issuers inherit silently.
  const isOrganizational = !issuer.projectId;

  const { data: projectsData } = useListProjects(
    { organizationId: organization.id },
    undefined,
    { enabled: isOrganizational },
  );
  const projects = useMemo(() => projectsData?.projects ?? [], [projectsData]);

  // For an organization-level issuer the client is either created at the
  // organization level (no project, attachable by every project) or downscoped
  // to a single project. Defaults to organization-level, the issuer's own scope.
  const [scope, setScope] = useState<"organization" | "project">(
    "organization",
  );
  const [projectId, setProjectId] = useState<string>(UNSELECTED_PROJECT);
  const [clientType, setClientType] = useState<ClientType>(defaultClientType);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [tokenEndpointAuthMethod, setTokenEndpointAuthMethod] = useState<
    CreateRemoteSessionClientFormTokenEndpointAuthMethod | ""
  >("");
  const [scopeOverride, setScopeOverride] = useState("");
  const [audienceOverride, setAudienceOverride] = useState("");

  // One mutation drives all client types. Its mutationFn does whatever async
  // work the chosen type needs — a DCR proxy registration pre-step, or a direct
  // create — so isPending/error are a single source of truth (no separate
  // registering flag or per-type mutation to combine). It returns the
  // unsupported DCR auth method, if any, for the success toast.
  const createMutation = useMutation({
    mutationFn: async (): Promise<{
      unsupportedDcrAuthMethod: string | null;
    }> => {
      const parsedScopes = parseScopes(scopeOverride);
      const trimmedAudience = audienceOverride.trim();
      // Project-specific issuers inherit their project (project_id omitted). For
      // an org-level issuer, omitting project_id creates an organization-level
      // client (no project); sending it downscopes to the chosen project.
      const projectIdForOrg =
        isOrganizational && scope === "project" ? projectId : undefined;

      // CIMD: the platform generates the client_id and hosts the metadata
      // document, so there are no credentials to collect.
      if (clientType === "cimd") {
        await client.organizationRemoteSessionIssuers.createCimdClient({
          createCimdOrganizationRemoteSessionClientForm: {
            remoteSessionIssuerId: issuer.id,
            projectId: projectIdForOrg,
            scope: parsedScopes.length > 0 ? parsedScopes : undefined,
            audience: trimmedAudience || undefined,
          },
        });
        return { unsupportedDcrAuthMethod: null };
      }

      // DCR proxies a registration to the upstream issuer for a fresh
      // client_id / client_secret; Manual uses what the operator typed.
      let credentials: {
        clientId: string;
        clientSecret?: string;
        tokenEndpointAuthMethod?: CreateRemoteSessionClientFormTokenEndpointAuthMethod;
      };
      let unsupportedDcrAuthMethod: string | null = null;
      if (clientType === "dcr") {
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
          unsupportedDcrAuthMethod = registered.tokenEndpointAuthMethod;
        }
        credentials = {
          clientId: registered.clientId,
          clientSecret: registered.clientSecret || undefined,
          tokenEndpointAuthMethod:
            narrowedDcrMethod ?? (tokenEndpointAuthMethod || undefined),
        };
      } else {
        credentials = {
          clientId: clientId.trim(),
          clientSecret: clientSecret.trim() || undefined,
          tokenEndpointAuthMethod: tokenEndpointAuthMethod || undefined,
        };
      }

      await client.organizationRemoteSessionIssuers.createClient({
        createOrganizationRemoteSessionClientForm: {
          remoteSessionIssuerId: issuer.id,
          projectId: projectIdForOrg,
          clientId: credentials.clientId,
          clientSecret: credentials.clientSecret,
          tokenEndpointAuthMethod: credentials.tokenEndpointAuthMethod,
          scope: parsedScopes.length > 0 ? parsedScopes : undefined,
          audience: trimmedAudience || undefined,
        },
      });

      return { unsupportedDcrAuthMethod };
    },
    onSuccess: async ({ unsupportedDcrAuthMethod }) => {
      await invalidateAllOrganizationRemoteSessionClients(queryClient, {
        refetchType: "all",
      });
      toast.success("Client created");
      if (unsupportedDcrAuthMethod) {
        toast.warning(
          `The issuer registered the client with an unsupported token endpoint auth method (${unsupportedDcrAuthMethod}); it was not stored on the client.`,
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
    setScope("organization");
    setProjectId(UNSELECTED_PROJECT);
    setClientId("");
    setClientSecret("");
    setTokenEndpointAuthMethod("");
    setScopeOverride("");
    setAudienceOverride("");
    setClientType(defaultClientType);
    resetCreateMutation();
  }, [open, defaultClientType, resetCreateMutation]);

  const projectChosen =
    !isOrganizational ||
    scope === "organization" ||
    projectId !== UNSELECTED_PROJECT;
  // Manual requires a client_id; DCR mints one during proxy registration.
  const credentialsReady =
    clientType === "manual" ? clientId.trim().length > 0 : true;
  const submittable = projectChosen && credentialsReady;

  const handleSubmit = () => {
    if (!submittable || submitting) return;
    createMutation.mutate();
  };

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
              <Stack gap={4}>
                <Stack gap={2}>
                  <Label className="text-muted-foreground text-xs">Scope</Label>
                  <Select
                    value={scope}
                    onValueChange={(value) =>
                      setScope(value as "organization" | "project")
                    }
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="organization">
                        All projects (organization-level)
                      </SelectItem>
                      <SelectItem value="project">Specific project</SelectItem>
                    </SelectContent>
                  </Select>
                  <Type muted small>
                    An organization-level client has no project and can be
                    attached by every project in the organization.
                  </Type>
                </Stack>

                {scope === "project" && (
                  <Stack gap={2}>
                    <Label className="text-muted-foreground text-xs">
                      Project
                    </Label>
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
                      The client will be scoped to this project in the
                      organization.
                    </Type>
                  </Stack>
                )}
              </Stack>
            ) : (
              <Type muted small>
                The client will be created in this provider's project.
              </Type>
            )}

            <ClientTypeFields
              availableTypes={clientTypes}
              clientType={clientType}
              onClientTypeChange={setClientType}
              clientId={clientId}
              clientSecret={clientSecret}
              tokenEndpointAuthMethod={tokenEndpointAuthMethod}
              onClientIdChange={setClientId}
              onClientSecretChange={setClientSecret}
              onTokenEndpointAuthMethodChange={setTokenEndpointAuthMethod}
            />

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
            onClick={handleSubmit}
          >
            <Button.Text>{submitting ? "Creating…" : "Create"}</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
