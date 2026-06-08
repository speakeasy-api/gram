import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useMcpEndpointUrl } from "@/hooks/useToolsetUrl";
import { remoteMcpRouteParam } from "@/lib/sources";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type {
  McpEndpoint,
  McpServer,
  RemoteMcpServer,
  ToolsetEntry,
} from "@gram/client/models/components";
import {
  useGetRemoteMcpServer,
  useListToolsets,
  useRemoteSessionClients,
} from "@gram/client/react-query/index.js";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { ArrowUpRight, Copy, ExternalLink } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

type OverviewTabProps = {
  mcpServer: McpServer | undefined;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
  onShowEndpoints: () => void;
  onShowAuthentication: () => void;
};

type StatusTone = "ready" | "needs-setup";
type RowStatus = { label: string; tone: StatusTone };

const READY_STATUS: RowStatus = { label: "READY", tone: "ready" };
const NEEDS_SETUP_STATUS: RowStatus = {
  label: "NEEDS SETUP",
  tone: "needs-setup",
};

/** "Ready" once set up, "Needs Setup" otherwise; undefined while loading. */
function readyStatus(
  isLoading: boolean,
  isReady: boolean,
): RowStatus | undefined {
  if (isLoading) return undefined;
  return isReady ? READY_STATUS : NEEDS_SETUP_STATUS;
}

export function OverviewTab({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
  onShowEndpoints,
  onShowAuthentication,
}: OverviewTabProps) {
  // The parent redirects on error, so an undefined `mcpServer` here always means
  // the top-level fetch is still in flight; show skeletons in place until then.
  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
      {mcpServer ? (
        <OverviewRows
          mcpServer={mcpServer}
          endpoints={endpoints}
          isLoadingEndpoints={isLoadingEndpoints}
          onShowEndpoints={onShowEndpoints}
          onShowAuthentication={onShowAuthentication}
        />
      ) : (
        <OverviewRowsSkeleton />
      )}
      <EnhanceSection />
    </div>
  );
}

function OverviewRows({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
  onShowEndpoints,
  onShowAuthentication,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
  onShowEndpoints: () => void;
  onShowAuthentication: () => void;
}) {
  const serverAddress = useServerAddressOverview(endpoints, isLoadingEndpoints);
  const authentication = useAuthenticationOverview(mcpServer);
  const source = useSourceOverview(mcpServer.remoteMcpServerId);

  return (
    <section>
      <Heading variant="h3" className="mb-1 font-semibold normal-case">
        Essentials
      </Heading>
      <EssentialsReadinessSummary
        serverAddress={serverAddress}
        authentication={authentication}
        source={source}
      />
      <div>
        <ServerAddressRow
          serverAddress={serverAddress}
          onConfigure={onShowEndpoints}
        />
        <AuthenticationOverviewRow
          authentication={authentication}
          onConfigure={onShowAuthentication}
        />
        {mcpServer.remoteMcpServerId ? (
          <SourceOverviewRow source={source} />
        ) : (
          // /x/mcp only renders mcp_servers-backed (remote MCP) servers, which
          // always carry a remoteMcpServerId, so this branch is currently
          // unreachable. Kept for when toolset-backed servers migrate here
          // (AGE-1902).
          <ToolsOverviewRow toolsetId={mcpServer.toolsetId} />
        )}
      </div>
    </section>
  );
}

type OverviewReadiness = {
  ready: boolean;
  loading: boolean;
};

type EssentialReadiness = {
  key: "server-url" | "authentication" | "source";
  ready: boolean;
  loading: boolean;
  incompleteMessage: string;
};

function EssentialsReadinessSummary({
  serverAddress,
  authentication,
  source,
}: {
  serverAddress: OverviewReadiness;
  authentication: OverviewReadiness;
  source: OverviewReadiness;
}) {
  const essentials: EssentialReadiness[] = [
    {
      key: "server-url",
      ready: serverAddress.ready,
      loading: serverAddress.loading,
      incompleteMessage: "Configure a server URL for clients to connect to.",
    },
    {
      key: "authentication",
      ready: authentication.ready,
      loading: authentication.loading,
      incompleteMessage:
        "Configure authentication to control who can use this MCP server.",
    },
    {
      key: "source",
      ready: source.ready,
      loading: source.loading,
      incompleteMessage: "Connect a source to provide tools for LLMs to use.",
    },
  ];

  const readyCount = essentials.filter((essential) => essential.ready).length;
  const isLoading = essentials.some((essential) => essential.loading);
  const nextEssential = essentials.find((essential) => !essential.ready);

  let message = "This MCP server is ready to be used.";
  if (isLoading) {
    message = "Checking essentials...";
  } else if (nextEssential) {
    message = nextEssential.incompleteMessage;
  }

  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-2">
      <div
        className="flex items-center gap-1"
        aria-label={`${readyCount} of ${essentials.length} essentials ready`}
      >
        {essentials.map((essential) => (
          <span
            key={essential.key}
            className={readinessSegmentClassName(essential)}
          />
        ))}
      </div>
      <Type muted small as="div" className="flex min-w-0 flex-wrap gap-x-3">
        <span>{message}</span>
      </Type>
    </div>
  );
}

function readinessSegmentClassName(essential: EssentialReadiness): string {
  let toneClass = "bg-amber-500";
  if (essential.loading) {
    toneClass = "bg-muted";
  } else if (essential.ready) {
    toneClass = "bg-green-500";
  }

  return `h-1.5 w-8 rounded-full ${toneClass}`;
}

type ServerAddressOverview = OverviewReadiness & {
  mcpUrl: string | undefined;
  installPageUrl: string | undefined;
  status: RowStatus | undefined;
};

function useServerAddressOverview(
  endpoints: McpEndpoint[],
  isLoadingEndpoints: boolean,
): ServerAddressOverview {
  const endpoint = useMemo(
    () => endpoints.find((e) => e.customDomainId) ?? endpoints[0],
    [endpoints],
  );
  const { mcpUrl: resolvedUrl } = useMcpEndpointUrl(endpoint);
  const fallbackUrl = endpoint?.slug
    ? `${getServerURL()}/mcp/${endpoint.slug}`
    : undefined;
  const mcpUrl = resolvedUrl ?? fallbackUrl;
  const ready = !isLoadingEndpoints && !!mcpUrl;

  return {
    ready,
    loading: isLoadingEndpoints,
    mcpUrl,
    installPageUrl: mcpUrl ? `${mcpUrl}/install` : undefined,
    status: readyStatus(isLoadingEndpoints, ready),
  };
}

type AuthenticationOverview = OverviewReadiness & {
  state: AuthState;
  secure: boolean;
  status: RowStatus | undefined;
};

function useAuthenticationOverview(
  mcpServer: McpServer,
): AuthenticationOverview {
  const userSessionIssuerId = mcpServer.userSessionIssuerId;
  const { data: clientsResult, isLoading } = useRemoteSessionClients(
    { userSessionIssuerId },
    undefined,
    { enabled: !!userSessionIssuerId },
  );

  const hasIssuer = !!userSessionIssuerId;
  const hasRemote = (clientsResult?.result.items.length ?? 0) > 0;
  const state = deriveAuthState({
    isPublic: mcpServer.visibility === "public",
    hasIssuer,
    hasRemote,
  });
  const loading = hasIssuer && isLoading;
  const secure = state !== "none";

  return {
    ready: !loading && secure,
    loading,
    state,
    secure,
    status: readyStatus(loading, secure),
  };
}

type SourceOverview = OverviewReadiness & {
  remoteMcpServer: RemoteMcpServer | undefined;
  status: RowStatus | undefined;
};

function useSourceOverview(
  remoteMcpServerId: string | undefined,
): SourceOverview {
  const id = remoteMcpServerId ?? "";
  const {
    data: remoteMcpServer,
    isLoading,
    isError,
  } = useGetRemoteMcpServer({ id }, undefined, {
    enabled: id !== "",
    throwOnError: false,
  });
  const loading = id !== "" && isLoading;
  const ready = !loading && !isError && !!remoteMcpServer;

  return {
    ready,
    loading,
    remoteMcpServer,
    status: readyStatus(loading, ready),
  };
}

function OverviewRowsSkeleton() {
  return (
    <section className="border-border border-y">
      <OverviewRowSkeleton />
      <OverviewRowSkeleton />
      <OverviewRowSkeleton />
    </section>
  );
}

function OverviewRowSkeleton() {
  return (
    <div className="border-border flex flex-col gap-4 border-b py-6 last:border-b-0 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 gap-5">
        <Skeleton className="mt-2 h-3 w-3 shrink-0 rounded-full" />
        <div className="min-w-0 flex-1 space-y-2">
          <div className="flex flex-wrap items-center gap-2.5">
            <Skeleton className="h-5 w-32" />
            <Skeleton className="h-5 w-20 rounded-full" />
          </div>
          <Skeleton className="h-4 w-96 max-w-full" />
        </div>
      </div>
      <Skeleton className="h-9 w-28 shrink-0 rounded-md sm:ml-6" />
    </div>
  );
}

function ServerAddressRow({
  serverAddress,
  onConfigure,
}: {
  serverAddress: ServerAddressOverview;
  onConfigure: () => void;
}) {
  const handleCopyUrl = () => {
    if (!serverAddress.mcpUrl) return;

    void navigator.clipboard
      .writeText(serverAddress.mcpUrl)
      .then(() => toast.success("URL copied to clipboard"))
      .catch(() => toast.error("Couldn't copy URL"));
  };

  const handleOpenInstallPage = () => {
    if (!serverAddress.installPageUrl) return;
    window.open(serverAddress.installPageUrl, "_blank", "noopener,noreferrer");
  };

  let description: ReactNode = "No endpoint configured yet.";
  if (serverAddress.loading) {
    description = <Skeleton className="h-4 w-96 max-w-full" />;
  } else if (serverAddress.mcpUrl) {
    description = (
      <span className="font-mono break-all">{serverAddress.mcpUrl}</span>
    );
  }

  let actions: ReactNode = (
    <Button variant="primary" onClick={onConfigure}>
      <Button.Text>Configure</Button.Text>
    </Button>
  );

  if (serverAddress.loading) {
    actions = <Skeleton className="h-9 w-28 rounded-md" />;
  } else if (serverAddress.ready) {
    actions = (
      <>
        <Button variant="secondary" onClick={handleCopyUrl}>
          <Button.LeftIcon>
            <Copy className="size-4" />
          </Button.LeftIcon>
          <Button.Text>Copy URL</Button.Text>
        </Button>
        <Button variant="secondary" onClick={handleOpenInstallPage}>
          <Button.Text>Install page</Button.Text>
          <Button.RightIcon>
            <ExternalLink className="size-4" />
          </Button.RightIcon>
        </Button>
      </>
    );
  }

  return (
    <OverviewRow
      title="Server URL"
      status={serverAddress.status}
      statusLoading={serverAddress.loading}
      description={description}
      actions={actions}
    />
  );
}
/**
 * Auth posture is a function of three inputs: whether Gram OAuth gates the
 * server (a user_session_issuer paired with a *private* server), whether an
 * upstream remote identity provider is attached, and public vs private
 * visibility. A user_session_issuer alone is not "secured": a public server
 * with no remote identity is open to anyone.
 */
type AuthState = "gram-only" | "gram-remote" | "remote-only" | "none";

const AUTH_ROW_COPY: Record<AuthState, string> = {
  "gram-only": "Requires Speakeasy organization access and MCP permissions.",
  "gram-remote":
    "Requires Speakeasy organization access, MCP permissions, and upstream login.",
  "remote-only":
    "Requires upstream login; Speakeasy organization roles do not apply.",
  none: "No authentication method configured - anyone with the URL can connect.",
};

function deriveAuthState({
  isPublic,
  hasIssuer,
  hasRemote,
}: {
  isPublic: boolean;
  hasIssuer: boolean;
  hasRemote: boolean;
}): AuthState {
  const gramGated = hasIssuer && !isPublic;

  if (gramGated && !hasRemote) return "gram-only";
  if (gramGated && hasRemote) return "gram-remote";
  if (isPublic && hasRemote) return "remote-only";
  return "none";
}

function AuthenticationOverviewRow({
  authentication,
  onConfigure,
}: {
  authentication: AuthenticationOverview;
  onConfigure: () => void;
}) {
  const actionLabel = authentication.secure ? "Manage" : "Configure";
  const actionVariant = authentication.secure ? "secondary" : "primary";

  return (
    <OverviewRow
      title="Authentication"
      status={authentication.status}
      statusLoading={authentication.loading}
      description={
        authentication.loading ? (
          <Skeleton className="h-4 w-[520px] max-w-full" />
        ) : (
          AUTH_ROW_COPY[authentication.state]
        )
      }
      actions={
        authentication.loading ? (
          <Skeleton className="h-9 w-28 rounded-md" />
        ) : (
          <Button variant={actionVariant} onClick={onConfigure}>
            <Button.Text>{actionLabel}</Button.Text>
          </Button>
        )
      }
    />
  );
}

function SourceOverviewRow({ source }: { source: SourceOverview }) {
  const routes = useRoutes();
  const navigate = useNavigate();
  const remoteMcpServer = source.remoteMcpServer;
  const trimmedName = remoteMcpServer?.name?.trim();

  let description: ReactNode = "Linked source could not be loaded.";
  if (source.loading) {
    description = <Skeleton className="h-4 w-80 max-w-full" />;
  } else if (source.ready) {
    description = trimmedName || "Remote MCP source";
  }

  const detail =
    source.ready && remoteMcpServer ? (
      <Type muted small mono as="div" className="break-all">
        {remoteMcpServer.url}
      </Type>
    ) : undefined;

  const actions =
    source.ready && remoteMcpServer ? (
      <Button
        variant="secondary"
        onClick={() =>
          navigate(
            routes.sources.source.href(
              "remotemcp",
              remoteMcpRouteParam(remoteMcpServer),
            ),
          )
        }
      >
        <Button.Text>View source</Button.Text>
        <Button.RightIcon>
          <ArrowUpRight className="size-4" />
        </Button.RightIcon>
      </Button>
    ) : undefined;

  return (
    <OverviewRow
      title="Source"
      status={source.status}
      statusLoading={source.loading}
      description={description}
      detail={detail}
      actions={actions}
    />
  );
}

function ToolsOverviewRow({ toolsetId }: { toolsetId: string | undefined }) {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { data: toolsetsResult, isLoading } = useListToolsets();
  const toolset = toolsetsResult?.toolsets.find(
    (t: ToolsetEntry) => t.id === toolsetId,
  );

  const ready = !isLoading && !!toolset;
  const displayName = toolset?.name?.trim() || toolset?.slug;

  let description: ReactNode = "Couldn't load the linked toolset.";
  if (isLoading) {
    description = <Skeleton className="h-4 w-64 max-w-full" />;
  } else if (displayName) {
    description = displayName;
  }

  const actions = toolset ? (
    <Button
      variant="secondary"
      onClick={() => navigate(routes.mcp.details.href(toolset.slug))}
    >
      <Button.Text>View tools</Button.Text>
      <Button.RightIcon>
        <ArrowUpRight className="size-4" />
      </Button.RightIcon>
    </Button>
  ) : undefined;

  return (
    <OverviewRow
      title="Tools"
      status={readyStatus(isLoading, ready)}
      statusLoading={isLoading}
      description={description}
      actions={actions}
    />
  );
}
function OverviewRow({
  title,
  status,
  statusLoading,
  description,
  detail,
  actions,
}: {
  title: string;
  status?: RowStatus;
  statusLoading?: boolean;
  description: ReactNode;
  detail?: ReactNode;
  actions?: ReactNode;
}) {
  let statusNode: ReactNode = null;
  if (status) {
    statusNode = <StatusBadge status={status} />;
  } else if (statusLoading) {
    statusNode = <Skeleton className="h-5 w-20 rounded-full" />;
  }

  return (
    <div className="border-border flex flex-col gap-4 border-b py-6 last:border-b-0 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 gap-5">
        <StatusDot status={status} loading={statusLoading} />
        <div className="min-w-0 flex-1">
          <div className="mb-1 flex flex-wrap items-center gap-2.5">
            <Type variant="subheading" as="h3">
              {title}
            </Type>
            {statusNode}
          </div>
          <Type muted small as="div" className="break-words">
            {description}
          </Type>
          {detail ? <div className="mt-1">{detail}</div> : null}
        </div>
      </div>
      {actions ? (
        <div className="flex flex-wrap items-center gap-2 pl-8 sm:ml-6 sm:shrink-0 sm:justify-end sm:pl-0">
          {actions}
        </div>
      ) : null}
    </div>
  );
}

function StatusDot({
  status,
  loading,
}: {
  status?: RowStatus;
  loading?: boolean;
}) {
  if (loading && !status) {
    return <Skeleton className="mt-2 h-3 w-3 shrink-0 rounded-full" />;
  }

  const colorClassName =
    status?.tone === "needs-setup" ? "bg-amber-500" : "bg-green-500";

  return (
    <span
      aria-hidden
      className={`mt-2 h-3 w-3 shrink-0 rounded-full ${colorClassName}`}
    />
  );
}

function StatusBadge({ status }: { status: RowStatus }) {
  const variant = status.tone === "ready" ? "success" : "warning";
  const toneClasses = STATUS_BADGE_TONE_CLASSES[status.tone];

  return (
    <Badge
      variant={variant}
      size="sm"
      background
      className={`shrink-0 px-2 py-0.5 ${toneClasses.root}`}
    >
      <Badge.Text
        className={`font-mono text-[11px] font-semibold tracking-wide uppercase ${toneClasses.text}`}
      >
        {status.label}
      </Badge.Text>
    </Badge>
  );
}

const STATUS_BADGE_TONE_CLASSES: Record<
  StatusTone,
  { root: string; text: string }
> = {
  ready: {
    root: "border-green-500/20! bg-green-500/10! dark:border-green-500/30! dark:bg-green-500/15!",
    text: "text-green-700! dark:text-green-300!",
  },
  "needs-setup": {
    root: "border-amber-500/25! bg-amber-500/10! dark:border-amber-500/30! dark:bg-amber-500/15!",
    text: "text-amber-700! dark:text-amber-300!",
  },
};

function EnhanceSection() {
  return (
    <section>
      <Heading variant="h3" className="mt-1 mb-1 font-semibold normal-case">
        Enhancements
      </Heading>
      <Type muted small className="mb-5">
        Optional items to customize the MCP server.
      </Type>
      <EnhancementRow
        title="Install page"
        description="Customize what users see when they visit the server's install page."
        configured={false}
        actionLabel="Customize"
        comingSoon
      />
    </section>
  );
}

function EnhancementRow({
  title,
  description,
  configured,
  actionLabel,
  onAction,
  comingSoon = false,
}: {
  title: string;
  description: string;
  configured: boolean;
  actionLabel: string;
  onAction?: () => void;
  comingSoon?: boolean;
}) {
  let actionNode: ReactNode = (
    <Button variant="secondary" onClick={onAction}>
      <Button.Text>{actionLabel}</Button.Text>
    </Button>
  );

  if (comingSoon) {
    actionNode = (
      // A disabled button swallows pointer events, so wrap it in a span
      // for the tooltip trigger to register hover.
      <SimpleTooltip tooltip="Coming Soon">
        <span tabIndex={0}>
          <Button variant="secondary" disabled>
            <Button.Text>{actionLabel}</Button.Text>
          </Button>
        </span>
      </SimpleTooltip>
    );
  }

  return (
    <div className="flex flex-col gap-4 py-2 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 gap-5">
        <EnhancementStatusDot configured={configured} />
        <div className="min-w-0 flex-1">
          <div className="mb-1 flex flex-wrap items-center gap-2.5">
            <Type variant="subheading" as="h3">
              {title}
            </Type>
            {comingSoon && (
              <Badge size="sm" variant="neutral" background>
                <Badge.Text>Coming Soon</Badge.Text>
              </Badge>
            )}
          </div>
          <Type muted small as="div" className="break-words">
            {description}
          </Type>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2 pl-8 sm:ml-6 sm:shrink-0 sm:justify-end sm:pl-0">
        {actionNode}
      </div>
    </div>
  );
}

function EnhancementStatusDot({ configured }: { configured: boolean }) {
  let colorClassName = "bg-muted-foreground/40";
  if (configured) {
    colorClassName = "bg-green-500";
  }

  return (
    <span
      aria-hidden
      className={`mt-2 h-3 w-3 shrink-0 rounded-full ${colorClassName}`}
    />
  );
}
