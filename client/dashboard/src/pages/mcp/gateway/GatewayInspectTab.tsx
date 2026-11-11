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
import { StatusDot } from "@/components/ui/StatusDot";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import { useUserSessionToken } from "@/hooks/useUserSessionToken";
import {
  platformEndpointSlug,
  useResolvedMcpServerUrl,
} from "@/hooks/useToolsetUrl";
import { enumPresentation } from "@/lib/enum";
import { DEFAULT_MODEL } from "@/lib/models";
import { getServerURL, mcpConnectionUrl } from "@/lib/utils";
import { PlaygroundChat } from "@/pages/playground/PlaygroundChat";
import { useRoutes } from "@/routes";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router";
import { InspectWorkbench, LensChoice } from "../inspect/InspectWorkbench";
import { runMcpTool } from "../inspect/runMcpTool";
import type { InspectTool } from "../inspect/types";
import { useToolHealth } from "../inspect/useToolHealth";
import { GatewayCodeExecution } from "./GatewayCodeExecution";
import { gatewayTabHref } from "./GatewayDetailsRouting";
import { GATEWAY_INSTRUCTIONS_SECTION_ID } from "./GatewaySettingsTab";
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

  const { rows } = useGatewayMemberRows(metaMcpServer.id);
  const { healthFor } = useToolHealth({ metaMcpServerId: metaMcpServer.id });

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
      mode === "code_mode" ? "reachable from code" : "behind execute_tool";
    const memberTools = members.flatMap((m) =>
      m.tools.map((tool) => ({ ...tool, group: `${m.name} · ${via}` })),
    );
    return [...surface, ...memberTools];
  }, [data, members, mode]);

  const tokens = data
    ? Math.round(JSON.stringify(data.tools).length / 4)
    : undefined;
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

  const defaultLabel = enumPresentation(
    modeLabels,
    metaMcpServer.discoveryMode,
    "Progressive",
  );
  const modeOptions: {
    value: ModeChoice;
    label: string;
    hint?: string;
    disabled?: boolean;
  }[] = [
    { value: "default", label: defaultLabel, hint: "gateway default" },
    { value: "direct", label: "Direct", hint: "every tool, flat" },
    { value: "progressive", label: "Progressive", hint: "five fixed tools" },
    ...(metaMcpServer.codeModeEnabled || choice === "code_mode"
      ? [
          {
            value: "code_mode" as const,
            label: "Code Mode",
            hint: "one Python tool",
            disabled: !metaMcpServer.codeModeEnabled,
          },
        ]
      : []),
  ];

  const lens = (
    <div className="flex flex-wrap items-baseline justify-between gap-3">
      <Text className="text-base">
        What <span className="font-medium">you</span> get in{" "}
        {hasIssuer ? (
          <LensChoice
            label="Preview a mode"
            value={choice}
            options={modeOptions}
            onChange={chooseMode}
            footnote={
              <>
                Previewing doesn&apos;t change the gateway. Change the default
                in <Link to={settingsHref}>Settings</Link>.
              </>
            }
          />
        ) : (
          <span className="font-medium">{defaultLabel}</span>
        )}
        {/* "Code Mode" already ends in the word. */}
        {(choice === "default" ? metaMcpServer.discoveryMode : choice) ===
        "code_mode"
          ? null
          : " mode"}
        {data ? (
          <span className="text-muted-foreground">
            {" "}
            · {data.tools.length} {data.tools.length === 1 ? "tool" : "tools"}
            {tokens !== undefined ? ` · ~${formatTokens(tokens)} tokens` : ""}
          </span>
        ) : null}
      </Text>
      {data?.instructions ? (
        <InstructionsLink
          instructions={data.instructions}
          editHref={`${settingsHref}#${GATEWAY_INSTRUCTIONS_SECTION_ID}`}
        />
      ) : null}
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
  const memberFailures =
    failedMembers.length > 0 ? (
      <p className="text-muted-foreground flex items-center gap-2 text-sm">
        <StatusDot tone="destructive" />
        Couldn&apos;t read tools from{" "}
        {failedMembers.map((m) => m.name).join(", ")}. Agents see the same
        error.
      </p>
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
    fallbackNote || shortfall || memberFailures || catalogFailure ? (
      <div className="flex flex-col gap-1">
        {fallbackNote}
        {shortfall}
        {catalogFailure}
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
  if (!connectUrl && !resolvingUrl) {
    state = (
      <InlineEmptyState
        icon="link"
        heading="This gateway has no address yet"
        description="Add one so clients, and this tab, can connect."
        action={
          <Button variant="secondary" asChild>
            <Link to={settingsHref}>
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
            <Link to={settingsHref}>
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

  const isLoading = resolvingUrl || token.isLoading || inspection.isLoading;

  return (
    <div className="flex min-h-0 w-full flex-1 flex-col">
      <InspectWorkbench
        lens={lens}
        tools={tools}
        isLoading={isLoading}
        state={state}
        notice={notice}
        healthFor={healthFor}
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
          connectUrl && codeConnection === configurationKey ? (
            <GatewayCodeExecution
              key={`${configurationKey}:${connectUrl}`}
              connectUrl={connectUrl}
              headers={headers}
              disabled={isLoading || inspection.isError}
            />
          ) : undefined
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
