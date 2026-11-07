import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { SetupBody } from "@/pages/catalog/provider-setup/SetupBody";
import { useRemoteMcpServerHeaders } from "@gram/client/react-query/remoteMcpServerHeaders";
import { snowflakeSelectionForUrl } from "@/pages/catalog/provider-setup/snowflake";
import {
  salesforceSelectionForUrl,
  salesforceInstanceConfiguration,
} from "@/pages/catalog/provider-setup/salesforce";
import type { State } from "@/pages/catalog/provider-setup/state";
import { useEffect, useReducer } from "react";
import { useProviderConnection } from "./useProviderConnection";
import { Link, Navigate, useNavigate } from "react-router";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { Skeleton } from "@/components/ui/Skeleton";
import { useRoutes } from "@/routes";
import { mcpServerRouteParam } from "@/lib/sources";
import type { McpServer } from "@gram/client/models/components/mcpserver";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer";
import { SetupContext } from "@/pages/catalog/provider-setup/setup-context";
import { names, reduce } from "@/pages/catalog/provider-setup/state";
import {
  type SetupProvider,
  providerProgressKey,
  setupProviderForUrl,
  loadProviderProgress,
  saveProviderProgress,
  providerSourceFingerprint,
} from "./provider-setup";

export function ProviderSetupPage({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const routes = useRoutes();
  const remoteId = mcpServer.remoteMcpServerId ?? "";
  const remote = useGetRemoteMcpServer({ id: remoteId }, undefined, {
    enabled: !!remoteId,
    throwOnError: false,
  });
  const overview = routes.mcp.details.overview.href(
    mcpServerRouteParam(mcpServer),
  );
  if (!remoteId) return <Navigate to={overview} replace />;
  if (remote.isLoading)
    return (
      <Page>
        <Page.Header>{null}</Page.Header>
        <Page.Body>
          <Skeleton className="h-64 w-full" />
        </Page.Body>
      </Page>
    );
  if (remote.isError)
    return (
      <Page>
        <Page.Header>{null}</Page.Header>
        <Page.Body>
          <Alert variant="error">
            Couldn’t load this server’s setup.{" "}
            <Button variant="secondary" onClick={() => void remote.refetch()}>
              <Button.Text>Retry</Button.Text>
            </Button>
          </Alert>
        </Page.Body>
      </Page>
    );
  const provider = setupProviderForUrl(remote.data?.url);
  if (!provider) return <Navigate to={overview} replace />;
  if (provider === "snowflake")
    return (
      <ProviderSetupFlow
        provider="snowflake"
        serverUrl={remote.data?.url ?? ""}
        mcpServer={mcpServer}
        overviewHref={overview}
        initialValues={snowflakeSelectionForUrl(remote.data?.url) ?? undefined}
      />
    );
  if (provider === "salesforce")
    return (
      <SalesforceSetupEntry
        serverUrl={remote.data?.url ?? ""}
        mcpServer={mcpServer}
        overviewHref={overview}
      />
    );
  return (
    <ProviderSetupFlow
      provider={provider}
      serverUrl={remote.data?.url ?? ""}
      mcpServer={mcpServer}
      overviewHref={overview}
    />
  );
}
function SalesforceSetupEntry({
  serverUrl,
  mcpServer,
  overviewHref,
}: {
  serverUrl: string;
  mcpServer: McpServer;
  overviewHref: string;
}): JSX.Element {
  const routes = useRoutes();
  const headers = useRemoteMcpServerHeaders(
    { remoteMcpServerId: mcpServer.remoteMcpServerId! },
    undefined,
    { throwOnError: false },
  );
  if (headers.isPending)
    return (
      <Page>
        <Page.Header>{null}</Page.Header>
        <Page.Body>
          <Skeleton className="h-64 w-full" />
        </Page.Body>
      </Page>
    );
  if (headers.isError)
    return (
      <Page>
        <Page.Header>{null}</Page.Header>
        <Page.Body>
          <Alert variant="error">
            Couldn’t load Salesforce connection configuration.{" "}
            <Button variant="secondary" onClick={() => void headers.refetch()}>
              <Button.Text>Retry</Button.Text>
            </Button>
          </Alert>
        </Page.Body>
      </Page>
    );
  let instanceUrl;
  try {
    instanceUrl = salesforceInstanceConfiguration(
      headers.data.headers,
    ).instanceUrl;
  } catch (error) {
    return (
      <Page>
        <Page.Header>{null}</Page.Header>
        <Page.Body>
          <Alert variant="error">
            {error instanceof Error
              ? error.message
              : "Review the MCP-Instance-URL header in identity settings."}{" "}
            <Link
              to={
                routes.mcp.details.settings.href(
                  mcpServerRouteParam(mcpServer),
                ) + "#authentication"
              }
            >
              Open identity settings
            </Link>
          </Alert>
        </Page.Body>
      </Page>
    );
  }
  return (
    <ProviderSetupFlow
      provider="salesforce"
      serverUrl={serverUrl}
      mcpServer={mcpServer}
      overviewHref={overviewHref}
      initialValues={{ ...salesforceSelectionForUrl(serverUrl), instanceUrl }}
      sourceInstance={instanceUrl}
    />
  );
}
function ProviderSetupFlow({
  provider,
  serverUrl,
  mcpServer,
  overviewHref,
  initialValues,
  sourceInstance,
}: {
  initialValues?: Partial<State["values"]>;
  sourceInstance?: string;
  provider: SetupProvider;
  serverUrl: string;
  mcpServer: McpServer;
  overviewHref: string;
}): JSX.Element {
  const organization = useOrganization(),
    project = useProject(),
    session = useSession();
  const storageKey = providerProgressKey(
    provider,
    session.user.id,
    organization.id,
    project.id,
    mcpServer.id,
  );
  return (
    <RetainedProviderSetup
      provider={provider}
      serverUrl={serverUrl}
      serverName={mcpServer.name ?? mcpServer.slug ?? names[provider]}
      key={storageKey}
      initialValues={initialValues}
      sourceInstance={sourceInstance}
      storageKey={storageKey}
      overviewHref={overviewHref}
      mcpServer={mcpServer}
    />
  );
}
function RetainedProviderSetup({
  provider,
  serverUrl,
  serverName,
  storageKey,
  overviewHref,
  mcpServer,
  initialValues,
  sourceInstance,
}: {
  initialValues?: Partial<State["values"]>;
  sourceInstance?: string;
  mcpServer: McpServer;
  provider: SetupProvider;
  serverUrl: string;
  serverName: string;
  storageKey: string;
  overviewHref: string;
}): JSX.Element {
  const navigate = useNavigate();
  const [s, dispatch] = useReducer(reduce, storageKey, (key) => {
    return loadProviderProgress(
      provider,
      key,
      initialValues,
      provider === "salesforce" || provider === "snowflake"
        ? providerSourceFingerprint(mcpServer.remoteMcpServerId, {
            url: serverUrl,
            instanceUrl: sourceInstance,
          })
        : undefined,
    );
  });
  useEffect(() => {
    saveProviderProgress(storageKey, s);
    if (s.screen === "success") void navigate(overviewHref, { replace: true });
  }, [storageKey, s, overviewHref, navigate]);
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [s.step, s.screen]);
  if (s.screen === "success") return <></>;
  return (
    <LiveSetupConnection
      s={s}
      dispatch={dispatch}
      mcpServer={mcpServer}
      serverUrl={serverUrl}
      sourceInstance={sourceInstance}
      serverName={serverName}
    />
  );
}
function LiveSetupConnection({
  sourceInstance,
  s,
  dispatch,
  mcpServer,
  serverUrl,
  serverName,
}: {
  sourceInstance?: string;
  s: Parameters<typeof useProviderConnection>[0];
  dispatch: Parameters<typeof useProviderConnection>[1];
  mcpServer: McpServer;
  serverUrl: string;
  serverName: string;
}): JSX.Element {
  const live = useProviderConnection(s, dispatch, mcpServer, {
    url: serverUrl,
    instanceUrl: sourceInstance,
  });
  const routes = useRoutes();
  const settingsHref =
    routes.mcp.details.settings.href(mcpServerRouteParam(mcpServer)) +
    "#authentication";
  return (
    <SetupContext
      value={{ s, dispatch: live.dispatch, live: { ...live, settingsHref } }}
    >
      <SetupBody serverUrl={serverUrl} serverName={serverName} />
    </SetupContext>
  );
}
