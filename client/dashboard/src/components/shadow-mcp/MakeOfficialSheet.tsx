import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Icon } from "@/components/ui/Icon";
import { Input } from "@/components/ui/Input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Spinner } from "@/components/ui/Spinner";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { UserSessionIssuerSelect } from "@/components/user-session-issuer-select";
import {
  PROJECT_SPECIFIC_ISSUER_VALUE,
  defaultCreationUserSessionIssuerValue,
} from "@/components/user-session-issuer-select.utils";
import { useProject } from "@/contexts/Auth";
import { useEffectiveUserSessionIssuers } from "@/hooks/useEffectiveUserSessionIssuers";
import { useRBAC } from "@/hooks/useRBAC";
import { useAgentCredentialFields } from "@/lib/remote-identity";
import { mcpServerRouteParam } from "@/lib/sources";
import { MCP_AUTHENTICATION_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection";
import { CreationIdentityChoice } from "@/pages/mcp/x/tabs/settings/sections/authentication/CreationIdentityChoice";
import type { RemoteMcpCreationIdentity } from "@/pages/sources/remote-mcp/configureCreatedIdentity";
import { useCreateRemoteMcpSource } from "@/pages/sources/remote-mcp/hooks";
import { useRoutes } from "@/routes";
import type { ProbeURLResult } from "@gram/client/models/components/probeurlresult.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { ExternalLink } from "lucide-react";
import { useEffect, useState } from "react";
import {
  shadowMCPCreatedDescription,
  shadowMCPCreatedTitle,
} from "./shadowMCPOfficialServer";
import { useSaveShadowMCPOfficialServer } from "./useSaveShadowMCPOfficialServer";

export type Created = {
  name: string;
  serverHref: string;
  settingsHref: string;
  /** The server was added but stays off until its sign-in is set up. */
  setupRequired: string | null;
  /** The server exists but is not linked: the link could not be saved, or
   *  the server stays off until its sign-in is set up. */
  linkFailed: boolean;
};

/**
 * Makes a Shadow MCP server official without leaving its page: Speakeasy
 * serves the same URL, and the new server is linked as the official version
 * in the same step. Everything is filled in from the checks; the identity
 * and sign-in choices most admins never change sit under Details.
 */
export function MakeOfficialSheet({
  server,
  probe,
  open,
  onOpenChange,
}: {
  server: ShadowMCPInventoryServer;
  /** The checks the page already ran; they decide the defaults. */
  probe: ProbeURLResult | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const shadowName =
    server.serverName || server.urlHost || server.canonicalServerUrl;
  const routes = useRoutes();
  const project = useProject();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canCreateIdentity =
    !rbacLoading && hasScope("project:write", project.id);
  const createRemote = useCreateRemoteMcpSource();
  const link = useSaveShadowMCPOfficialServer();
  const issuerQuery = useEffectiveUserSessionIssuers();
  const agentCredential = useAgentCredentialFields();

  const advertisesOAuth =
    probe?.outcome === "authentication_required" &&
    !!probe.protectedResourceMetadataUrl;
  const [name, setName] = useState(shadowName);
  const [identityMode, setIdentityMode] = useState<RemoteMcpCreationIdentity>(
    advertisesOAuth ? "user" : "none",
  );
  const [issuerSelection, setIssuerSelection] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState<Created | null>(null);

  // Each opening starts from the server's own defaults.
  useEffect(() => {
    if (!open) return;
    setName(shadowName);
    setIdentityMode(advertisesOAuth ? "user" : "none");
    setIssuerSelection(null);
    setError(null);
    setCreated(null);
  }, [open, shadowName, advertisesOAuth]);

  const defaultIssuer = defaultCreationUserSessionIssuerValue(
    issuerQuery.organizationIssuers,
  );
  const selectedIssuer = issuerSelection ?? defaultIssuer;
  const issuerBlocked =
    issuerQuery.isLoading || issuerQuery.isError || selectedIssuer === "";
  const credentialMissing =
    identityMode === "agent" &&
    agentCredential.authorizationValue.trim() === "";
  const cannotCreateIdentity = identityMode === "user" && !canCreateIdentity;

  const identitySummary =
    identityMode === "user"
      ? "Each person signs in as themselves."
      : identityMode === "agent"
        ? "Every caller uses one shared credential."
        : "Speakeasy adds no sign-in.";

  const submit = async () => {
    if (submitting || issuerBlocked) return;
    setSubmitting(true);
    setError(null);
    try {
      const { identityConfiguration, mcpServer } =
        await createRemote.mutateAsync({
          name: name.trim() === "" ? undefined : name.trim(),
          url: server.canonicalServerUrl,
          userSessionIssuerId:
            selectedIssuer === PROJECT_SPECIFIC_ISSUER_VALUE
              ? undefined
              : selectedIssuer,
          identityMode,
          agentAuthorization:
            identityMode === "agent"
              ? agentCredential.authorizationValue.trim()
              : undefined,
        });
      const setupRequired =
        identityConfiguration.status === "setup-required"
          ? identityConfiguration.message
          : null;
      // A server waiting on its sign-in stays off, and an off server cannot
      // be linked, so the link waits until the admin turns it on.
      let linkFailed = setupRequired !== null;
      if (!linkFailed) {
        try {
          await link.save({
            serverUrl: server.canonicalServerUrl,
            mcpServerId: mcpServer.id,
            allowDirectAccess: false,
          });
        } catch {
          linkFailed = true;
        }
      }
      const routeParam = mcpServerRouteParam(mcpServer);
      setCreated({
        name: mcpServer.name || name.trim() || shadowName,
        serverHref: routes.mcp.details.overview.href(routeParam),
        settingsHref: `${routes.mcp.details.settings.href(routeParam)}#${MCP_AUTHENTICATION_SECTION_ID}`,
        setupRequired,
        linkFailed,
      });
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "The server could not be added.",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        // Closing mid-save would hide whether the server was made.
        if (submitting) return;
        onOpenChange(next);
      }}
    >
      <SheetContent
        side="right"
        className="flex w-[520px] flex-col sm:max-w-[520px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            {created ? shadowMCPCreatedTitle(created) : "Make official"}
          </SheetTitle>
          <SheetDescription>
            {created
              ? shadowMCPCreatedDescription(created, shadowName)
              : `Speakeasy serves ${shadowName} and links it as the official version. Direct connections are then blocked, and agents are sent to the official server.`}
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          {created ? (
            <CreatedSummary created={created} shadowName={shadowName} />
          ) : (
            <Stack gap={5}>
              <Stack gap={1}>
                <label
                  htmlFor="make-official-name"
                  className="text-sm leading-none font-medium"
                >
                  Name
                </label>
                <Input
                  id="make-official-name"
                  value={name}
                  onChange={setName}
                  disabled={submitting}
                />
              </Stack>
              <Stack gap={1}>
                <Text variant="small" className="font-medium">
                  URL
                </Text>
                <Text muted small className="font-mono break-all">
                  {server.canonicalServerUrl}
                </Text>
              </Stack>
              <Stack gap={1}>
                <Text variant="small" className="font-medium">
                  Sign-in
                </Text>
                <Text muted small>
                  {identitySummary}
                </Text>
              </Stack>

              <Collapsible className="border-border border-t pt-4">
                <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm transition-colors [&[data-state=open]>svg]:rotate-90">
                  <Icon name="chevron-right" className="size-3.5" aria-hidden />
                  Details
                </CollapsibleTrigger>
                <CollapsibleContent className="mt-4 space-y-5">
                  <CreationIdentityChoice
                    value={identityMode}
                    onChange={setIdentityMode}
                    credential={agentCredential}
                    upstreamName={name.trim() || shadowName}
                    advertisesOAuth={advertisesOAuth}
                    authenticationRequired={
                      probe?.outcome === "authentication_required"
                    }
                    canCreateIdentity={canCreateIdentity}
                    rbacLoading={rbacLoading}
                  />
                  <Stack gap={1}>
                    <label className="text-sm leading-none font-medium">
                      User session issuer
                    </label>
                    <UserSessionIssuerSelect
                      issuers={issuerQuery.organizationIssuers}
                      value={selectedIssuer}
                      onValueChange={setIssuerSelection}
                      includeProjectSpecific
                      disabled={issuerQuery.isLoading || issuerQuery.isError}
                    />
                    <Text muted small>
                      Where people sign in to Speakeasy before they reach this
                      server. The organization&apos;s issuer suits most setups.
                    </Text>
                  </Stack>
                </CollapsibleContent>
              </Collapsible>

              {error ? (
                <Alert variant="error" dismissible={false}>
                  {error}
                </Alert>
              ) : null}
            </Stack>
          )}
        </div>

        <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
          {created ? (
            <Button variant="primary" onClick={() => onOpenChange(false)}>
              <Button.Text>Done</Button.Text>
            </Button>
          ) : (
            <>
              <Button
                variant="secondary"
                disabled={submitting}
                onClick={() => onOpenChange(false)}
              >
                <Button.Text>Cancel</Button.Text>
              </Button>
              <Button
                variant="primary"
                disabled={
                  submitting ||
                  issuerBlocked ||
                  credentialMissing ||
                  cannotCreateIdentity
                }
                onClick={() => void submit()}
              >
                {submitting ? (
                  <Button.LeftIcon>
                    <Spinner className="mr-0 size-4" />
                  </Button.LeftIcon>
                ) : null}
                <Button.Text>
                  {submitting ? "Making official" : "Make official"}
                </Button.Text>
              </Button>
            </>
          )}
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

export function CreatedSummary({
  created,
  shadowName,
}: {
  created: Created;
  shadowName: string;
}): JSX.Element {
  return (
    <Stack gap={4}>
      <div className="border-border flex items-center justify-between gap-3 border p-4">
        <div className="flex min-w-0 items-center gap-3">
          <Icon
            name="circle-check"
            className="text-default-success size-5 shrink-0"
          />
          <div className="min-w-0">
            <Text variant="small" className="truncate font-medium">
              {created.name}
            </Text>
            <Text muted small>
              {created.linkFailed
                ? `Added, but not yet linked to ${shadowName}.`
                : `Linked to ${shadowName}`}
            </Text>
          </div>
        </div>
        <Button variant="secondary" size="sm" asChild>
          <a href={created.serverHref} target="_blank" rel="noreferrer">
            <Button.Text>Open</Button.Text>
            <Button.RightIcon>
              <ExternalLink className="size-3.5" />
            </Button.RightIcon>
          </a>
        </Button>
      </div>
      {created.setupRequired ? (
        <Alert variant="warning" dismissible={false}>
          The server stays off until its sign-in is set up.{" "}
          {created.setupRequired}{" "}
          <a
            href={created.settingsHref}
            target="_blank"
            rel="noreferrer"
            className="underline"
          >
            Finish setup
          </a>
          , turn the server on, then link it from this page.
        </Alert>
      ) : (
        <Text muted small>
          Open it to check its tools and try it in the Playground. Back on this
          page you can switch the link off while you test, or remove it.
        </Text>
      )}
      {created.linkFailed && !created.setupRequired ? (
        <Alert variant="error" dismissible={false}>
          The link could not be saved. Link it from this server&apos;s page
          under Advanced.
        </Alert>
      ) : null}
    </Stack>
  );
}
