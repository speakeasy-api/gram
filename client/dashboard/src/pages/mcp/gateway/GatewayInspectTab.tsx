import { SETTINGS_GROUPS } from "@/components/detail/settings-groups";
import { AUTHORIZATION_SERVERS_FIELD_ID } from "@/pages/mcp/x/tabs/settings/sections/authentication/RemoteIdentityProvidersField";
import { useHideInsightsDock } from "@/components/insights-context";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Alert } from "@/components/ui/Alert";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { StatusDot } from "@/components/ui/StatusDot";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useSession } from "@/contexts/Auth";
import { useUserSessionToken } from "@/hooks/useUserSessionToken";
import {
  platformEndpointSlug,
  useResolvedMcpServerUrl,
} from "@/hooks/useToolsetUrl";
import { mcpServerRouteParam } from "@/lib/sources";
import { DEFAULT_MODEL } from "@/lib/models";
import { getServerURL, mcpConnectionUrl } from "@/lib/utils";
import { PlaygroundChat } from "@/pages/playground/PlaygroundChat";
import { useRoutes } from "@/routes";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router";
import { InspectWorkbench } from "../inspect/InspectWorkbench";
import { runMcpTool } from "../inspect/runMcpTool";
import type { InspectTool } from "../inspect/types";
import { useToolHealth } from "../inspect/useToolHealth";
import { GatewayCodeExecution } from "./GatewayCodeExecution";
import { GatewayConnectStep } from "./GatewayConnectStep";
import {
  GATEWAY_SIGN_IN_WHY,
  NEEDS_CREDENTIAL,
  NEEDS_GATEWAY_SIGN_IN,
  useGatewayMemberStates,
} from "./useGatewayMemberStates";
import { gatewayTabHref } from "./GatewayDetailsRouting";
import { MCP_SERVER_URL_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/ServerUrlSection";
import {
  GATEWAY_AUTHENTICATION_SECTION_ID,
  GATEWAY_DISCOVERY_SECTION_ID,
  GATEWAY_INSTRUCTIONS_SECTION_ID,
} from "./GatewaySettingsTab";

/** The discovery mode section's anchor in `GatewayDiscoverySection`. */
import { useGatewayInspection } from "./useGatewayInspection";
import { useGatewayMemberRows } from "./useGatewayMemberRows";
import { useGatewayMemberTools } from "./useGatewayMemberTools";

type Mode = "direct" | "progressive" | "code_mode";
type ModeChoice = "default" | Mode;

const modeLabels: Record<Mode, string> = {
  progressive: "Progressive",
  direct: "Direct",
  code_mode: "Code Mode",
};

const modeHints: Record<Mode, string> = {
  direct: "Every tool listed up front",
  progressive: "Agents look tools up as they need them",
  code_mode: "Agents write code that calls tools",
};

/** The gateway's saved mode; anything unknown serves as Progressive. */
function savedMode(mode: MetaMcpServer["discoveryMode"]): Mode {
  if (mode === "direct") return "direct";
  if (mode === "code_mode") return "code_mode";
  return "progressive";
}

/** The tool that runs a member's tool behind a Progressive gateway. */
const EXECUTE_TOOL = "execute_tool";

export function GatewayInspectTab(
  props: Parameters<typeof GatewayInspectConnection>[0],
): JSX.Element {
  const session = useSession();
  // The Chat tab hosts its own chat runtime; see InspectTab.
  useHideInsightsDock();
  return (
    <GatewayInspectConnection
      key={`${props.metaMcpServer.id}:${session.user.id}`}
      {...props}
    />
  );
}

function GatewayInspectConnection({
  metaMcpServer,
  endpoints,
  isLoadingEndpoints,
}: {
  metaMcpServer: MetaMcpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
}): JSX.Element {
  const routes = useRoutes();
  const settingsHref = gatewayTabHref(routes, metaMcpServer.id, "settings");

  // The previewed mode lives in the URL so a link reproduces the view. It
  // travels in the minted session token, so changing it reconnects.
  const [searchParams, setSearchParams] = useSearchParams();
  const requested = searchParams.get("discovery_mode");
  const choice: ModeChoice =
    requested === "direct" ||
    requested === "progressive" ||
    requested === "code_mode"
      ? requested
      : "default";
  const [version, setVersion] = useState<string>(choice);
  // Connecting accounts is optional: skipping shows the tools the viewer can
  // already use, and a line above them leads back to connecting.
  const [skippedConnect, setSkippedConnect] = useState(false);
  const chooseMode = (next: ModeChoice) => {
    setSearchParams(
      (current) => {
        const params = new URLSearchParams(current);
        if (next === "default") params.delete("discovery_mode");
        else params.set("discovery_mode", next);
        return params;
      },
      { replace: true },
    );
    setVersion(crypto.randomUUID());
  };
  const discoveryMode = choice === "default" ? undefined : choice;
  const configurationKey = `${choice}:${version}:${metaMcpServer.discoveryMode}`;

  const { mcpUrl, loading: resolvingUrl } = useResolvedMcpServerUrl(
    endpoints,
    isLoadingEndpoints,
  );
  const connectUrl = useMemo(() => mcpConnectionUrl(mcpUrl), [mcpUrl]);
  const hasIssuer = !!metaMcpServer.userSessionIssuerId;

  // Connect as the signed-in user so the tab shows what this person's agents
  // get. Minting requires the same mcp:connect the runtime enforces.
  const token = useUserSessionToken({
    target: {
      kind: "metaMcpServer",
      id: metaMcpServer.id,
      discoveryMode,
      connectionVersion: version,
    },
    userSessionIssuerId: metaMcpServer.userSessionIssuerId,
    throwOnError: false,
  });
  const headers = useMemo(
    () =>
      token.accessToken
        ? { Authorization: `Bearer ${token.accessToken}` }
        : undefined,
    [token.accessToken],
  );
  const ready =
    !resolvingUrl &&
    !!connectUrl &&
    !token.isLoading &&
    !token.error &&
    (!hasIssuer || !!token.accessToken);

  const inspection = useGatewayInspection(connectUrl, {
    headers,
    enabled: ready,
    configurationKey,
  });
  const data = inspection.data;
  const mode = data?.discoveryMode;

  // Code Mode serves one tool, so the member catalog comes from a second,
  // Progressive session — the same members the program can reach.
  const catalogToken = useUserSessionToken({
    target: {
      kind: "metaMcpServer",
      // No id, no mint: the catalog session exists only in Code Mode.
      id: mode === "code_mode" ? metaMcpServer.id : undefined,
      discoveryMode: "progressive",
      connectionVersion: `${version}:catalog`,
    },
    userSessionIssuerId: metaMcpServer.userSessionIssuerId,
    throwOnError: false,
  });
  const catalogHeaders = useMemo(
    () =>
      catalogToken.accessToken
        ? { Authorization: `Bearer ${catalogToken.accessToken}` }
        : undefined,
    [catalogToken.accessToken],
  );
  const catalog = useGatewayInspection(connectUrl, {
    headers: catalogHeaders,
    enabled: mode === "code_mode" && !!catalogHeaders,
    configurationKey: `${configurationKey}:catalog`,
  });

  const memberSource =
    mode === "progressive"
      ? { servers: data?.servers, headers }
      : mode === "code_mode"
        ? { servers: catalog.data?.servers, headers: catalogHeaders }
        : { servers: undefined, headers: undefined };
  const members = useGatewayMemberTools(
    connectUrl,
    memberSource.headers,
    memberSource.servers,
    {
      enabled: mode === "progressive" || mode === "code_mode",
      cacheKey: configurationKey,
    },
  );

  const { rows, isLoading: isLoadingRows } = useGatewayMemberRows(
    metaMcpServer.id,
  );
  const { healthFor, isLoading: isHealthLoading } = useToolHealth({
    metaMcpServerId: metaMcpServer.id,
  });
  // Members that need the viewer's own account: connecting lists them again.
  // The same viewer status Overview and the header read, so all three agree.
  const { signIn, stateFor } = useGatewayMemberStates(
    metaMcpServer,
    endpoints,
    isLoadingEndpoints,
    rows,
    {
      checkTools: "cache",
      onConnected: () => {
        inspection.refetch();
        catalog.refetch();
        members.forEach((m) => m.refetch());
      },
    },
  );

  // Keep the Code Mode editor across token refreshes; reconnects reset it.
  const [codeConnection, setCodeConnection] = useState<string>();
  useEffect(() => {
    if (mode === "code_mode" && !inspection.isError) {
      setCodeConnection(configurationKey);
    }
  }, [mode, inspection.isError, configurationKey]);

  const memberToolNames = useMemo(
    () => new Set(members.flatMap((m) => m.tools.map((t) => t.name))),
    [members],
  );
  const tools = useMemo<InspectTool[]>(() => {
    if (!data) return [];
    const surface: InspectTool[] = data.tools.map((tool) => ({
      name: tool.name,
      description: tool.description,
      inputSchema: tool.inputSchema,
      group: mode === "direct" ? undefined : "Gateway tools",
    }));
    if (mode === "direct") return surface;
    const via =
      mode === "code_mode"
        ? "callable from code"
        : "reached through the gateway";
    const memberTools = members.flatMap((m) =>
      m.tools.map((tool) => ({ ...tool, group: `${m.name} · ${via}` })),
    );
    return [...surface, ...memberTools];
  }, [data, members, mode]);

  // A rough size of the tool descriptions an agent loads on connect.
  const tokens = data
    ? Math.round(JSON.stringify(data.tools).length / 4)
    : undefined;
  const surfaceCount = data?.tools.length ?? 0;
  const memberToolCount = members.reduce((n, m) => n + m.tools.length, 0);
  const servedMembers =
    mode === "code_mode" ? catalog.data?.servers : data?.servers;
  const unserved =
    servedMembers !== undefined ? rows.length - servedMembers.length : 0;
  const failedMembers = members.filter((m) => m.error);
  // list_servers is best-effort, so a failure leaves the catalog undefined
  // rather than failing the tab; say so instead of showing no members.
  const catalogFailed =
    mode === "progressive"
      ? !!data && data.servers === undefined
      : mode === "code_mode"
        ? !!catalogToken.error ||
          catalog.isError ||
          (!!catalog.data && catalog.data.servers === undefined)
        : false;

  const defaultMode = savedMode(metaMcpServer.discoveryMode);
  const shownMode: Mode = choice === "default" ? defaultMode : choice;
  const modeOptions = (["direct", "progressive", "code_mode"] as const)
    .filter(
      (value) =>
        value !== "code_mode" ||
        metaMcpServer.codeModeEnabled ||
        shownMode === "code_mode",
    )
    .map((value) => ({
      value,
      label:
        value === defaultMode ? (
          <>
            {modeLabels[value]}
            <span className="sr-only"> (gateway default)</span>
          </>
        ) : (
          modeLabels[value]
        ),
      tooltip:
        value === "code_mode" && !metaMcpServer.codeModeEnabled
          ? "Code Mode isn't turned on for this gateway"
          : value === defaultMode
            ? `Gateway default. ${modeHints[value]}`
            : modeHints[value],
      disabled: value === "code_mode" && !metaMcpServer.codeModeEnabled,
    }));

  const counts = data ? (
    <>
      <SimpleTooltip
        tooltip={`Agents load ${surfaceCount === 1 ? "this tool's description" : "these tool descriptions"} as soon as they connect: about ${formatTokens(tokens ?? 0)} tokens of their context.`}
      >
        <span className="decoration-muted-foreground/60 cursor-help underline decoration-dotted underline-offset-4">
          {surfaceCount} {mode === "direct" ? "" : "gateway "}
          {surfaceCount === 1 ? "tool" : "tools"}
        </span>
      </SimpleTooltip>
      {mode === "direct"
        ? null
        : ` · ${memberToolCount} server ${memberToolCount === 1 ? "tool" : "tools"} ${mode === "code_mode" ? "callable from code" : "reached through them"}`}
    </>
  ) : null;

  // One row: the mode on the left (a preview is marked beside it), the
  // facts and instructions on the right. The control names the mode, so no
  // sentence repeats it.
  const lens = (
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
      <div className="flex items-center gap-3">
        {/* The mode travels in the minted session token, so only a gateway
            with sign-in can preview another one. */}
        {hasIssuer ? (
          <SegmentedControl
            ariaLabel="Preview a mode"
            value={shownMode}
            options={modeOptions}
            onChange={(next) =>
              chooseMode(next === defaultMode ? "default" : next)
            }
            className="h-8"
          />
        ) : (
          <Text className="text-base font-medium">
            {modeLabels[shownMode]}
            {shownMode === "code_mode" ? null : " mode"}
          </Text>
        )}
        {choice !== "default" ? (
          <SimpleTooltip
            tooltip={`Only you see this preview. Agents get ${modeLabels[defaultMode]}, the gateway's saved mode.`}
          >
            <Link
              to={`${settingsHref}#${GATEWAY_DISCOVERY_SECTION_ID}`}
              className="text-muted-foreground hover:text-foreground text-xs whitespace-nowrap underline-offset-2 hover:underline"
            >
              Preview · saved mode is {modeLabels[defaultMode]}
            </Link>
          </SimpleTooltip>
        ) : null}
      </div>
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
        {counts ? (
          <Text muted small>
            {counts}
          </Text>
        ) : null}
        {data?.instructions ? (
          <InstructionsLink
            instructions={data.instructions}
            editHref={`${settingsHref}#${GATEWAY_INSTRUCTIONS_SECTION_ID}`}
          />
        ) : null}
      </div>
    </div>
  );

  const fallbackNote =
    (choice === "code_mode" ||
      (choice === "default" && metaMcpServer.discoveryMode === "code_mode")) &&
    !metaMcpServer.codeModeEnabled ? (
      <Text muted small>
        Code Mode isn&apos;t available for this gateway, so this connection uses
        Progressive.
      </Text>
    ) : null;
  const shortfall =
    unserved > 0 ? (
      <Text muted small>
        {hasIssuer
          ? `${unserved} of ${rows.length} servers on this gateway aren't served to you. Private servers need your access, and disabled servers are never served.`
          : `${unserved} of ${rows.length} servers aren't served. This gateway has no sign-in, so every caller is anonymous and private servers are hidden.`}
      </Text>
    ) : null;
  // Every member the viewer still has to connect, in any mode: Direct reads
  // no member on its own, so the shared member status supplies them too.
  const unconnected = [
    ...failedMembers.filter((m) => signIn.needsSignIn(m.slug)),
    ...rows.flatMap((row) => {
      const slug = row.server?.slug ?? row.member.mcpServerSlug;
      if (!slug || stateFor(row)?.kind !== "sign_in") return [];
      if (failedMembers.some((m) => m.slug === slug)) return [];
      return [{ slug, name: row.server?.name ?? slug }];
    }),
  ];
  // Tools behind these servers can't be counted, so the summary counts the
  // servers: the ones whose tools couldn't be read, plus any the shared
  // member status says need sign-in or are unavailable.
  const attentionSlugs = new Set([
    ...failedMembers.map((m) => m.slug),
    ...rows
      .filter((row) => {
        const kind = stateFor(row)?.kind;
        return kind === "sign_in" || kind === "unavailable";
      })
      .map((row) => row.server?.slug ?? row.member.mcpServerSlug),
  ]);
  const broken = failedMembers.filter((m) => !signIn.needsSignIn(m.slug));
  const memberServer = (slug: string) =>
    rows.find((row) => (row.server?.slug ?? row.member.mcpServerSlug) === slug)
      ?.server;
  // One banner per kind of problem, one row per server with its own action,
  // so a list of failures never runs together on one line.
  const unconnectedNames =
    unconnected.length === 1
      ? `${unconnected[0]!.name} isn't connected`
      : `${unconnected.length} servers aren't connected`;
  const memberSignIn =
    unconnected.length > 0 && skippedConnect ? (
      <p className="text-muted-foreground flex flex-wrap items-center gap-2 text-sm">
        <StatusDot tone="warning" />
        {unconnectedNames}, so {unconnected.length === 1 ? "its" : "their"}{" "}
        tools aren&apos;t listed.
        <button
          type="button"
          onClick={() => setSkippedConnect(false)}
          className="text-foreground underline underline-offset-2"
        >
          Connect
        </button>
      </p>
    ) : null;
  const memberFailures =
    broken.length > 0 ? (
      <Alert
        variant="error"
        title={
          broken.length === 1
            ? `Couldn't read ${broken[0]!.name}'s tools`
            : `Couldn't read tools from ${broken.length} servers`
        }
      >
        <p className="text-muted-foreground">
          Agents connecting through this gateway get the same error.
        </p>
        <ul className="mt-2 flex flex-col">
          {broken.map((m) => {
            const server = memberServer(m.slug);
            const reason = memberFailureReason(m.error);
            return (
              <li
                key={m.slug}
                className="flex items-start justify-between gap-3 py-1.5"
              >
                <span className="flex min-w-0 flex-col">
                  <span className="text-foreground">{m.name}</span>
                  {reason.text ? (
                    <span className="text-muted-foreground text-xs break-words">
                      {reason.text}
                    </span>
                  ) : null}
                </span>
                {reason.needsGatewaySignIn ? (
                  <Link
                    to={`${settingsHref}#${AUTHORIZATION_SERVERS_FIELD_ID}`}
                    className="text-foreground shrink-0 underline underline-offset-2"
                  >
                    Set up sign-in
                  </Link>
                ) : server ? (
                  <Link
                    to={
                      reason.needsCredential
                        ? `${routes.mcp.details.settings.href(mcpServerRouteParam(server))}#settings-sign-in`
                        : routes.mcp.details.overview.href(
                            mcpServerRouteParam(server),
                          )
                    }
                    className="text-foreground shrink-0 underline underline-offset-2"
                  >
                    {reason.needsCredential ? "Set credential" : "Open server"}
                  </Link>
                ) : null}
              </li>
            );
          })}
        </ul>
        <Button
          size="sm"
          variant="secondary"
          className="mt-2"
          onClick={() => broken.forEach((m) => m.refetch())}
        >
          <Button.Text>Try again</Button.Text>
        </Button>
      </Alert>
    ) : null;
  const catalogFailure = catalogFailed ? (
    <p className="text-muted-foreground flex flex-wrap items-center gap-2 text-sm">
      <StatusDot tone="destructive" />
      {mode === "code_mode" && catalogToken.error
        ? "Couldn't sign in to list this gateway's servers, so their tools aren't shown."
        : "Couldn't list this gateway's servers, so their tools aren't shown."}
      <button
        type="button"
        onClick={
          mode !== "code_mode"
            ? inspection.refetch
            : catalogToken.error
              ? catalogToken.refetch
              : catalog.refetch
        }
        className="text-foreground underline underline-offset-2"
      >
        Try again
      </button>
    </p>
  ) : null;
  const notice =
    fallbackNote ||
    shortfall ||
    memberSignIn ||
    memberFailures ||
    catalogFailure ? (
      <div className="flex flex-col gap-1">
        {fallbackNote}
        {shortfall}
        {catalogFailure}
        {memberSignIn}
        {memberFailures}
      </div>
    ) : undefined;
  // The chat prefers the Speakeasy origin over a custom domain; a gateway
  // with only a custom-domain address chats there.
  const platformSlug = platformEndpointSlug(endpoints);
  const chatUrl = platformSlug
    ? `${getServerURL()}/mcp/${platformSlug}`
    : mcpUrl;

  let state: ReactNode;
  if (metaMcpServer.visibility === "disabled") {
    state = (
      <InlineEmptyState
        icon="power-off"
        heading="This gateway is turned off"
        description="Agents can't reach it or its servers' tools until it's turned on."
        action={
          <Button variant="secondary" asChild>
            {/* Availability sits at the top of Settings' General area. */}
            <Link to={`${settingsHref}#${SETTINGS_GROUPS.general.id}`}>
              <Button.Text>Turn on in settings</Button.Text>
            </Link>
          </Button>
        }
      />
    );
  } else if (!connectUrl && !resolvingUrl) {
    state = (
      <InlineEmptyState
        icon="link"
        heading="This gateway has no address yet"
        description="Add one so clients, and this tab, can connect."
        action={
          <Button variant="secondary" asChild>
            <Link to={`${settingsHref}#${MCP_SERVER_URL_SECTION_ID}`}>
              <Button.Text>Open settings</Button.Text>
            </Link>
          </Button>
        }
      />
    );
  } else if (inspection.needsAuth) {
    state = (
      <InlineEmptyState
        icon="lock"
        heading="This gateway didn't accept the dashboard's connection"
        description="Reload to try again, or review the gateway's authentication."
        action={
          <Button variant="secondary" asChild>
            <Link to={`${settingsHref}#${GATEWAY_AUTHENTICATION_SECTION_ID}`}>
              <Button.Text>Review authentication</Button.Text>
            </Link>
          </Button>
        }
      />
    );
  } else if (inspection.isError || token.error) {
    state = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't read this gateway"
        description={
          (token.error ?? inspection.error)?.message ??
          "The gateway didn't answer."
        }
        action={
          <Button
            variant="secondary"
            onClick={token.error ? token.refetch : inspection.refetch}
          >
            <Button.Text>Try again</Button.Text>
          </Button>
        }
      />
    );
  }

  if (
    !state &&
    !skippedConnect &&
    unconnected.length > 0 &&
    !inspection.isLoading
  ) {
    state = (
      <GatewayConnectStep
        members={unconnected.map((m) => ({
          slug: m.slug,
          name: m.name,
          mcpServerId: memberServer(m.slug)?.id,
        }))}
        onConnect={signIn.connect}
        onSkip={() => setSkippedConnect(true)}
        availableCount={tools.length}
      />
    );
  }

  const isLoading =
    metaMcpServer.visibility !== "disabled" &&
    (resolvingUrl || token.isLoading || inspection.isLoading);

  return (
    <div className="flex min-h-0 w-full flex-1 flex-col">
      <InspectWorkbench
        lens={lens}
        tools={tools}
        isLoading={isLoading}
        state={state}
        notice={notice}
        healthFor={healthFor}
        isHealthLoading={isHealthLoading}
        unavailableStat={{
          label: "Servers need attention",
          value: attentionSlugs.size,
          // Counted from member rows and the viewer's status too, so it stays
          // in its checking state until both have settled.
          isLoading:
            isLoadingRows ||
            signIn.isLoading ||
            members.some((m) => m.isLoading) ||
            rows.some((row) => stateFor(row)?.kind === "checking"),
        }}
        accessHref={gatewayTabHref(routes, metaMcpServer.id, "team-access")}
        runTool={
          connectUrl && mode !== "code_mode"
            ? (tool, args) =>
                memberToolNames.has(tool.name)
                  ? runMcpTool({
                      url: connectUrl,
                      headers,
                      name: EXECUTE_TOOL,
                      args: { name: tool.name, arguments: args },
                    })
                  : runMcpTool({
                      url: connectUrl,
                      headers,
                      name: tool.name,
                      args,
                    })
            : undefined
        }
        runner={
          // Keyed on the connection, not the live mode, so a token refresh
          // that briefly clears the inspection keeps the program.
          connectUrl && codeConnection === configurationKey
            ? (selected) => (
                <GatewayCodeExecution
                  key={`${configurationKey}:${connectUrl}`}
                  connectUrl={connectUrl}
                  headers={headers}
                  // Only member tools are callable from code.
                  tool={
                    selected && memberToolNames.has(selected.name)
                      ? selected
                      : undefined
                  }
                  disabled={isLoading || inspection.isError}
                />
              )
            : undefined
        }
        chat={
          chatUrl && data && !inspection.isError ? (
            <PlaygroundChat
              mcpUrl={chatUrl}
              gatewayToken={token.accessToken}
              model={DEFAULT_MODEL}
              compact
              environmentSlug={null}
            />
          ) : undefined
        }
      />
    </div>
  );
}

function formatTokens(tokens: number): string {
  return tokens >= 1000 ? `${(tokens / 1000).toFixed(1)}k` : String(tokens);
}

/** The instructions every client receives on connect, one click away. */
function InstructionsLink({
  instructions,
  editHref,
}: {
  instructions: string;
  editHref: string;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="text-muted-foreground hover:text-foreground text-sm underline-offset-2 hover:underline"
      >
        Server instructions
      </button>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent className="w-full gap-0 sm:max-w-lg">
          <SheetHeader className="border-b">
            <SheetTitle>Server instructions</SheetTitle>
            <SheetDescription>
              Every client receives this text when it connects.
            </SheetDescription>
          </SheetHeader>
          <div className="flex flex-1 flex-col gap-4 overflow-y-auto p-4">
            <pre className="font-mono text-xs leading-relaxed break-words whitespace-pre-wrap">
              {instructions}
            </pre>
            <div>
              <Button variant="secondary" size="sm" asChild>
                <Link to={editHref}>
                  <Button.Text>Edit instructions</Button.Text>
                </Link>
              </Button>
            </div>
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}

/**
 * Why a member's tools couldn't be read, in plain words. The gateway's own
 * message is kept when nothing better is known, so the cause is never hidden.
 */
function memberFailureReason(error: string | undefined): {
  text?: string;
  needsCredential: boolean;
  needsGatewaySignIn?: boolean;
} {
  if (!error) return { needsCredential: false };
  if (NEEDS_GATEWAY_SIGN_IN.test(error))
    return {
      text: GATEWAY_SIGN_IN_WHY,
      needsCredential: false,
      needsGatewaySignIn: true,
    };
  if (NEEDS_CREDENTIAL.test(error))
    return {
      text: "Needs a shared credential before the gateway can call it. An admin sets it in the server's sign-in settings.",
      needsCredential: true,
    };
  return { text: error.replace(/\s+/g, " ").trim(), needsCredential: false };
}
