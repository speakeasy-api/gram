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
import { useSdkClient } from "@/contexts/Sdk";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import {
  invalidateMcpServerQueries,
  mcpServerVisibilityUpdateForm,
} from "@/lib/mcp-server-visibility";
import { useAgentCredentialFields } from "@/lib/remote-identity";
import { mcpServerRouteParam } from "@/lib/sources";
import { CatalogLogo } from "@/pages/catalog/CatalogLogo";
import type { CatalogServer } from "@/pages/catalog/hooks";
import { collectibleHeaders } from "@/pages/catalog/remotes";
import { useRemoteMcpInstallWorkflow } from "@/pages/catalog/useRemoteMcpInstallWorkflow";
import { AuthenticationSectionBody } from "@/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection";
import { useMcpServerAuthTarget } from "@/pages/mcp/x/tabs/settings/sections/authentication/authTarget";
import { CreationIdentityChoice } from "@/pages/mcp/x/tabs/settings/sections/authentication/CreationIdentityChoice";
import type { RemoteMcpCreationIdentity } from "@/pages/sources/remote-mcp/configureCreatedIdentity";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { ProbeURLResult } from "@gram/client/models/components/probeurlresult.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import {
  buildGetMcpServerQuery,
  useGetMcpServer,
} from "@gram/client/react-query/getMcpServer.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { type Created, CreatedSummary } from "./MakeOfficialSheet";
import { shadowMCPCatalogRemote } from "./shadowMCPOfficialServer";
import { useSaveShadowMCPOfficialServer } from "./useSaveShadowMCPOfficialServer";

type Stage =
  | { kind: "configure" }
  | { kind: "sign-in"; mcpServerId: string; message: string }
  | { kind: "done"; created: Created };

/**
 * Makes a Shadow MCP server official from its Speakeasy catalog entry,
 * without leaving the Shadow MCP page. The catalog version is preferred over
 * a custom server: it carries the entry's name, logo, documentation and the
 * headers it needs. A provider that needs a hand-registered client is set up
 * in the same sheet, with the server's own Identity settings, before the
 * server is turned on and linked.
 */
export function CatalogOfficialSheet({
  server,
  entry,
  probe,
  open,
  onOpenChange,
}: {
  server: ShadowMCPInventoryServer;
  entry: CatalogServer;
  probe: ProbeURLResult | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const shadowName =
    server.serverName || server.urlHost || server.canonicalServerUrl;
  const remote = shadowMCPCatalogRemote(
    entry.remotes,
    server.canonicalServerUrl,
  );
  const routes = useRoutes();
  const link = useSaveShadowMCPOfficialServer();
  const project = useProject();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canCreateIdentity =
    !rbacLoading && hasScope("project:write", project.id);
  const agentCredential = useAgentCredentialFields();

  // The URL check knows whether the upstream asks for OAuth even when the
  // catalog does not say how Speakeasy registers with it.
  const advertisesOAuth =
    probe?.outcome === "authentication_required" &&
    !!probe.protectedResourceMetadataUrl;
  const entryTitle = entry.title || shadowName;

  // Only the endpoint people already use is installed. Both inputs stay
  // stable across renders, or the workflow would start over each time.
  const installServers = useMemo(
    () => (open && remote ? [{ ...entry, remotes: [remote] }] : []),
    [open, entry, remote],
  );
  const initialConfiguration = useMemo(
    () => ({
      name: entryTitle,
      identityMode: (advertisesOAuth
        ? "user"
        : "none") as RemoteMcpCreationIdentity,
    }),
    [entryTitle, advertisesOAuth],
  );
  const workflow = useRemoteMcpInstallWorkflow({
    servers: installServers,
    initialConfiguration,
    autoSelectRemotes: true,
  });

  const [stage, setStage] = useState<Stage>({ kind: "configure" });
  const [error, setError] = useState<string | null>(null);
  const [working, setWorking] = useState(false);
  const handled = useRef(false);

  const resetWorkflow = workflow.reset;
  useEffect(() => {
    if (!open) return;
    setStage({ kind: "configure" });
    setError(null);
    handled.current = false;
    resetWorkflow();
    // Each opening starts over; the workflow's reset is stable per opening.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const config =
    workflow.phase === "configure" ? workflow.serverConfigs[0] : undefined;
  const updateConfig =
    workflow.phase === "configure" ? workflow.updateServerConfig : undefined;
  const agentValue = agentCredential.authorizationValue;
  useEffect(() => {
    updateConfig?.(0, { agentAuthorization: agentValue });
    // updateServerConfig is stable; follow the credential the admin types.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentValue]);

  const finish = async (
    mcpServerId: string,
    slug: string | undefined,
    name: string,
  ) => {
    let linkFailed = false;
    try {
      await link.save({
        serverUrl: server.canonicalServerUrl,
        mcpServerId,
        allowDirectAccess: false,
      });
    } catch {
      linkFailed = true;
    }
    const routeParam = mcpServerRouteParam({ id: mcpServerId, slug });
    setStage({
      kind: "done",
      created: {
        name,
        serverHref: routes.mcp.details.overview.href(routeParam),
        settingsHref: routes.mcp.details.settings.href(routeParam),
        setupRequired: null,
        linkFailed,
      },
    });
  };

  // The install reports its result as a phase; act on it once.
  const finishRef = useRef(finish);
  finishRef.current = finish;
  useEffect(() => {
    if (workflow.phase !== "complete" || handled.current) return;
    handled.current = true;
    const status = workflow.statuses[0];
    if (!status?.mcpServerId) {
      setError(status?.error ?? "The server could not be added.");
      setStage({ kind: "configure" });
      setWorking(false);
      return;
    }
    if (status.setupRequired) {
      setStage({
        kind: "sign-in",
        mcpServerId: status.mcpServerId,
        message: status.setupRequired,
      });
      setWorking(false);
      return;
    }
    void finishRef
      .current(status.mcpServerId, status.mcpServerParam, status.name)
      .finally(() => setWorking(false));
  }, [workflow]);

  const add = async () => {
    if (workflow.phase !== "configure" || !workflow.canInstall) return;
    setError(null);
    setWorking(true);
    try {
      await workflow.startInstall();
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "The server could not be added.",
      );
      setWorking(false);
    }
  };

  const busy = working || workflow.phase === "installing";
  const headers = remote ? collectibleHeaders(remote) : [];
  const missingHeader = headers.some(
    (header) =>
      header.isRequired &&
      !(
        config?.headerValues[`${remote?.url} ${header.name.toLowerCase()}`] ??
        ""
      ).trim(),
  );
  const identityMode = config?.identityMode ?? "none";
  const identitySummary =
    identityMode === "user"
      ? advertisesOAuth && !entry.supportsDcr && !entry.supportsCimd
        ? "Each person signs in as themselves. Its provider needs a client you register, which you enter next."
        : "Each person signs in as themselves."
      : identityMode === "agent"
        ? "Every caller uses one shared credential."
        : "Speakeasy adds no sign-in.";

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        // Closing mid-install would hide whether the server was made.
        if (busy) return;
        onOpenChange(next);
      }}
    >
      <SheetContent
        side="right"
        className="flex w-[680px] flex-col sm:max-w-[680px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            {stage.kind === "done"
              ? "Official server linked"
              : stage.kind === "sign-in"
                ? "Set up sign-in"
                : "Add from the catalog"}
          </SheetTitle>
          <SheetDescription>
            {stage.kind === "done"
              ? `Agents that call ${shadowName} directly are now sent here.`
              : stage.kind === "sign-in"
                ? `${entryTitle} is added but stays off until people can sign in. Enter the client you registered with its provider, save it, then turn the server on.`
                : `Speakeasy adds the catalog version of ${entryTitle} and links it as the official version of ${shadowName}.`}
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          {stage.kind === "done" ? (
            <CreatedSummary created={stage.created} shadowName={shadowName} />
          ) : null}

          {stage.kind === "sign-in" ? (
            <SignInSetup mcpServerId={stage.mcpServerId} />
          ) : null}

          {stage.kind === "configure" ? (
            <Stack gap={5}>
              <div className="border-border flex items-center gap-3 border p-3">
                <CatalogLogo server={entry} className="size-9" />
                <div className="min-w-0">
                  <Text variant="small" className="truncate font-medium">
                    {entryTitle}
                  </Text>
                  <Text muted small className="truncate font-mono text-xs">
                    {entry.registrySpecifier}
                  </Text>
                </div>
              </div>
              {remote ? null : (
                <Alert variant="error" dismissible={false}>
                  The catalog entry no longer lists this URL.
                </Alert>
              )}
              <Stack gap={1}>
                <label
                  htmlFor="catalog-official-name"
                  className="text-sm leading-none font-medium"
                >
                  Name
                </label>
                <Input
                  id="catalog-official-name"
                  value={config?.name ?? entryTitle}
                  onChange={(name) => updateConfig?.(0, { name })}
                  disabled={busy}
                />
              </Stack>
              {headers.map((header) => (
                <Stack gap={1} key={header.name}>
                  <label
                    htmlFor={`catalog-header-${header.name}`}
                    className="text-sm leading-none font-medium"
                  >
                    {header.name}
                    {header.isRequired ? "" : " (optional)"}
                  </label>
                  <Input
                    id={`catalog-header-${header.name}`}
                    type={header.isSecret ? "password" : "text"}
                    placeholder={header.placeholder}
                    value={
                      config?.headerValues[
                        `${remote?.url} ${header.name.toLowerCase()}`
                      ] ?? ""
                    }
                    onChange={(value) =>
                      workflow.phase === "configure" && remote
                        ? workflow.setHeaderValue(
                            0,
                            remote.url,
                            header.name,
                            value,
                          )
                        : undefined
                    }
                    disabled={busy}
                  />
                  {header.description ? (
                    <Text muted small>
                      {header.description}
                    </Text>
                  ) : null}
                </Stack>
              ))}
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
                    onChange={(next) =>
                      updateConfig?.(0, { identityMode: next })
                    }
                    credential={agentCredential}
                    upstreamName={config?.name.trim() || entryTitle}
                    advertisesOAuth={
                      probe?.outcome === "authentication_required"
                    }
                    authenticationRequired={
                      probe?.outcome === "authentication_required"
                    }
                    canCreateIdentity={canCreateIdentity}
                    rbacLoading={rbacLoading}
                  />
                </CollapsibleContent>
              </Collapsible>

              {workflow.phase === "configure" &&
              workflow.installBlockedReason ? (
                <Alert variant="error" dismissible={false}>
                  {workflow.installBlockedReason}
                </Alert>
              ) : null}
              {error ? (
                <Alert variant="error" dismissible={false}>
                  {error}
                </Alert>
              ) : null}
            </Stack>
          ) : null}
        </div>

        <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
          {stage.kind === "done" ? (
            <Button variant="primary" onClick={() => onOpenChange(false)}>
              <Button.Text>Done</Button.Text>
            </Button>
          ) : null}
          {stage.kind === "sign-in" ? (
            <TurnOnAndLink
              mcpServerId={stage.mcpServerId}
              onLinked={(mcpServer) =>
                void finish(
                  mcpServer.id,
                  mcpServer.slug,
                  mcpServer.name || entryTitle,
                )
              }
            />
          ) : null}
          {stage.kind === "configure" ? (
            <>
              <Button
                variant="secondary"
                disabled={busy}
                onClick={() => onOpenChange(false)}
              >
                <Button.Text>Cancel</Button.Text>
              </Button>
              <Button
                variant="primary"
                disabled={
                  busy ||
                  !remote ||
                  workflow.phase !== "configure" ||
                  !workflow.canInstall ||
                  missingHeader ||
                  (identityMode === "user" && !canCreateIdentity) ||
                  (identityMode === "agent" && agentValue.trim() === "")
                }
                onClick={() => void add()}
              >
                {busy ? (
                  <Button.LeftIcon>
                    <Spinner className="mr-0 size-4" />
                  </Button.LeftIcon>
                ) : null}
                <Button.Text>{busy ? "Adding" : "Add and link"}</Button.Text>
              </Button>
            </>
          ) : null}
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

/** The new server's own Identity settings, inside the sheet. */
function SignInSetup({ mcpServerId }: { mcpServerId: string }) {
  const serverQuery = useGetMcpServer({ id: mcpServerId }, undefined, {
    throwOnError: false,
  });
  if (!serverQuery.data) {
    return (
      <div className="text-muted-foreground flex items-center gap-2 text-sm">
        <Spinner className="size-4" />
        Loading the server
      </div>
    );
  }
  return <SignInSetupBody mcpServer={serverQuery.data} />;
}

function SignInSetupBody({ mcpServer }: { mcpServer: McpServer }) {
  const target = useMcpServerAuthTarget(mcpServer);
  return <AuthenticationSectionBody target={target} />;
}

/**
 * Turns the new server on, then links it. A server people cannot sign in to
 * would fail every call, so the admin saves the client above first.
 */
function TurnOnAndLink({
  mcpServerId,
  onLinked,
}: {
  mcpServerId: string;
  onLinked: (mcpServer: McpServer) => void;
}) {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const update = useUpdateMcpServerMutation();
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);

  const turnOn = async () => {
    setPending(true);
    setFailed(null);
    try {
      const latest = await queryClient.fetchQuery({
        ...buildGetMcpServerQuery(client, { id: mcpServerId }),
        staleTime: 0,
      });
      const updated =
        latest.visibility === "disabled"
          ? await update.mutateAsync({
              request: {
                updateMcpServerForm: mcpServerVisibilityUpdateForm(
                  latest,
                  "private",
                ),
              },
            })
          : latest;
      await invalidateMcpServerQueries(queryClient);
      onLinked(updated);
    } catch (err) {
      setFailed(
        err instanceof Error
          ? err.message
          : "The server could not be turned on.",
      );
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="flex w-full items-center justify-between gap-3">
      <Text muted small className="text-destructive">
        {failed ?? ""}
      </Text>
      <Button
        variant="primary"
        disabled={pending}
        onClick={() => void turnOn()}
      >
        {pending ? (
          <Button.LeftIcon>
            <Spinner className="mr-0 size-4" />
          </Button.LeftIcon>
        ) : null}
        <Button.Text>{pending ? "Turning on" : "Turn on and link"}</Button.Text>
      </Button>
    </div>
  );
}
