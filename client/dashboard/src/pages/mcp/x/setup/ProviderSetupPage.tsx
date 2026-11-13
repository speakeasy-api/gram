import { useRemoteMcpServerHeaders } from "@gram/client/react-query/remoteMcpServerHeaders";
import {
  salesforceSelectionForUrl,
  salesforceInstanceConfiguration,
} from "@/pages/catalog/provider-setup/salesforce";
import type { State } from "@/pages/catalog/provider-setup/state";
import { useEffect, useReducer, useRef } from "react";
import { useProviderConnection } from "./useProviderConnection";
import { Link, Navigate, useNavigate } from "react-router";
import { Check } from "lucide-react";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { SetupGuideLink } from "@/components/setup-guide/SetupGuideLink";
import { PageEyebrow } from "@/components/page-eyebrow";
import { useConfettiBurst } from "@/components/icon-confetti";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Skeleton } from "@/components/ui/Skeleton";
import { Alert } from "@/components/ui/Alert";
import { useRoutes } from "@/routes";
import { mcpServerRouteParam } from "@/lib/sources";
import type { McpServer } from "@gram/client/models/components/mcpserver";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer";
import {
  SetupContext,
  useSetup,
} from "@/pages/catalog/provider-setup/setup-context";
import {
  names,
  reduce,
  ready,
  steps,
} from "@/pages/catalog/provider-setup/state";
import { ProviderTask } from "@/pages/catalog/provider-setup/ProviderTasks";
import { GuideSetupPage } from "./GuideSetupPage";
import { taskTitle } from "@/pages/catalog/provider-setup/provider-data";
import { ConnectionTask } from "@/pages/catalog/provider-setup/ConnectionTask";
import {
  type SetupProvider,
  providerProgressKey,
  setupProviderForUrl,
  loadProviderProgress,
  saveProviderProgress,
  salesforceSourceFingerprint,
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
      <>
        <Skeleton className="h-64 w-full" />
      </>
    );
  if (remote.isError)
    return (
      <>
        <Alert variant="error">
          Couldn’t load this server’s setup.{" "}
          <Button variant="secondary" onClick={() => void remote.refetch()}>
            <Button.Text>Retry</Button.Text>
          </Button>
        </Alert>
      </>
    );
  const provider = setupProviderForUrl(remote.data?.url);
  // Providers without a guided flow get their written guide, full page.
  if (!provider)
    return (
      <GuideSetupPage
        serverUrl={remote.data?.url}
        mcpServer={mcpServer}
        overviewHref={overview}
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
      <>
        <Skeleton className="h-64 w-full" />
      </>
    );
  if (headers.isError)
    return (
      <>
        <Alert variant="error">
          Couldn’t load Salesforce connection configuration.{" "}
          <Button variant="secondary" onClick={() => void headers.refetch()}>
            <Button.Text>Retry</Button.Text>
          </Button>
        </Alert>
      </>
    );
  let instanceUrl;
  try {
    instanceUrl = salesforceInstanceConfiguration(
      headers.data.headers,
    ).instanceUrl;
  } catch (error) {
    return (
      <>
        <Alert variant="error">
          {error instanceof Error
            ? error.message
            : "Review the MCP-Instance-URL header in identity settings."}{" "}
          <Link
            to={
              routes.mcp.details.settings.href(mcpServerRouteParam(mcpServer)) +
              "#authentication"
            }
          >
            Open identity settings
          </Link>
        </Alert>
      </>
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
      provider === "salesforce"
        ? salesforceSourceFingerprint(mcpServer.remoteMcpServerId, {
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
  // Each step starts at the top of the Setup modal, which is what scrolls.
  useEffect(() => {
    document.querySelector("[data-setup-scroll]")?.scrollTo?.(0, 0);
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
function StepRail(): JSX.Element {
  const { s, dispatch } = useSetup();
  return (
    <nav
      aria-label={`${names[s.provider]} setup steps`}
      className="row-start-1 min-w-0 lg:sticky lg:top-6 lg:col-start-2 lg:row-start-1"
    >
      <ol className="flex flex-wrap gap-x-[18px] gap-y-3 lg:block">
        {steps(s).map((step, index) => {
          let completed = s.done.includes(step.id);
          if (step.id === "credentials") completed = s.saved;
          if (step.id === "test") completed = s.tested;
          return (
            <li
              key={step.id}
              className="relative text-xs lg:pb-3 lg:after:absolute lg:after:top-6 lg:after:bottom-0.5 lg:after:left-[11px] lg:after:w-px lg:after:bg-border lg:last:pb-0 lg:last:after:hidden"
            >
              <button
                type="button"
                className="flex items-center gap-3 text-left"
                aria-current={s.step === step.id ? "step" : undefined}
                onClick={() => dispatch({ type: "goto", step: step.id })}
              >
                <span
                  className={`relative z-1 grid size-6 shrink-0 place-items-center border font-mono text-[11px] ${s.step === step.id ? "border-foreground bg-foreground text-background" : "bg-card text-muted-foreground"}`}
                >
                  {completed ? (
                    <Check className="size-3" aria-hidden />
                  ) : (
                    index + 1
                  )}
                </span>
                <span className="text-sm">{step.title}</span>
              </button>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
function SetupBody({
  serverUrl,
  serverName,
}: {
  serverUrl: string;
  serverName: string;
}): JSX.Element {
  const { s, dispatch, live } = useSetup();
  const name = names[s.provider];
  const complete = ready(s) && s.authorized && s.tested;
  const canComplete = complete && !live.pending && !live.loading && !live.error;
  const { canvasRef, burst } = useConfettiBurst();
  // Initial validation is already complete when reopening; only celebrate a new pass.
  const celebratedValidation = useRef(complete);
  useEffect(() => {
    if (!complete) celebratedValidation.current = false;
    else if (
      s.step === "test" &&
      canComplete &&
      !celebratedValidation.current
    ) {
      celebratedValidation.current = true;
      burst({ particleCount: 45, ticks: 90 });
    }
  }, [complete, canComplete, s.step, burst]);
  let content = <ProviderTask serverName={serverName} />;
  if (s.step === "test") content = <ConnectionTask />;
  return (
    // Inside the server's detail frame: a heading for the current task, then
    // the task beside its step rail.
    <div className="flex flex-col gap-6 [&_button:focus-visible]:outline-2 [&_button:focus-visible]:outline-offset-2 [&_button:focus-visible]:outline-[var(--border-focus)] [&_button:focus-visible]:outline-solid">
      <div className="grid grid-cols-1 items-end gap-6 lg:grid-cols-[minmax(0,768px)_minmax(170px,1fr)] lg:gap-8 xl:gap-14">
        <div className="min-w-0">
          <PageEyebrow area={`${name} Setup`} className="mb-2" />
          <Heading
            variant="h2"
            className="text-[28px] leading-9 tracking-[-.03em]"
          >
            {taskTitle(s)}
          </Heading>
        </div>
        <SetupGuideLink
          serverUrl={serverUrl}
          variant="secondary"
          className="bg-card mb-1 justify-self-start"
        />
      </div>
      <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[minmax(0,768px)_minmax(170px,1fr)] lg:gap-8 xl:gap-14">
        <section
          aria-label={`Current ${name} setup task`}
          className="@container/task relative isolate min-w-0 max-w-[768px] text-sm leading-[1.65] [&_summary]:cursor-pointer [&_strong]:font-semibold"
        >
          <canvas
            ref={canvasRef}
            hidden={s.step !== "test"}
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 -z-10 size-full"
          />
          {content}
          {live.error && (s.step !== "test" || live.errorStep === "save") && (
            <Alert variant="error" className="mt-5">
              <p>{live.error}</p>
              {live.recovery === "settings" ? (
                <Link to={live.settingsHref ?? "../settings#authentication"}>
                  Open identity settings
                </Link>
              ) : (
                live.loading && (
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={live.retryLoad}
                  >
                    <Button.Text>Retry loading saved credentials</Button.Text>
                  </Button>
                )
              )}
            </Alert>
          )}
          <>
            <div className="mt-[30px] flex justify-end gap-2 border-t pt-[22px]">
              {s.step !== "begin" && (
                <Button
                  variant="tertiary"
                  onClick={() => dispatch({ type: "back" })}
                >
                  <Button.Text>Back</Button.Text>
                </Button>
              )}
              {s.step === "test" ? (
                <Button
                  variant="primary"
                  disabled={!canComplete}
                  onClick={() => dispatch({ type: "complete" })}
                >
                  <Button.Text>Complete setup</Button.Text>
                </Button>
              ) : (
                <Button
                  variant="primary"
                  disabled={
                    !!live.pending ||
                    live.loading ||
                    (s.provider === "salesforce" &&
                      s.values.path === "support" &&
                      s.step === "support")
                  }
                  onClick={live.next}
                >
                  <Button.Text>
                    {live.pending === "save"
                      ? "Saving…"
                      : s.step === "begin"
                        ? "Get started"
                        : "Continue"}
                  </Button.Text>
                </Button>
              )}
            </div>
          </>
        </section>
        <StepRail />
      </div>
    </div>
  );
}
