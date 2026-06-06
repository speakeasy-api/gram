import { CodeBlock } from "@/components/code";
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
  ToolsetEntry,
} from "@gram/client/models/components";
import {
  useGetRemoteMcpServer,
  useListToolsets,
  useRemoteSessionClients,
} from "@gram/client/react-query/index.js";
import { Badge, Button } from "@speakeasy-api/moonshine";
import {
  AlertTriangle,
  ArrowUpRight,
  BookOpen,
  GlobeIcon,
  Network,
  Shield,
  ShieldCheck,
  Sparkles,
  Wrench,
} from "lucide-react";
import {
  useMemo,
  type ComponentType,
  type ReactNode,
  type SVGProps,
} from "react";
import { useNavigate } from "react-router";

type OverviewTabProps = {
  mcpServer: McpServer | undefined;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
  onShowEndpoints: () => void;
  onShowAuthentication: () => void;
};

type StatusTone = "ready" | "needs-setup";
type CardStatus = { label: string; tone: StatusTone };

/** "Ready" once set up, "Needs Setup" otherwise; undefined while loading. */
function readyStatus(
  isLoading: boolean,
  isReady: boolean,
): CardStatus | undefined {
  if (isLoading) return undefined;
  return isReady
    ? { label: "Ready", tone: "ready" }
    : { label: "Needs Setup", tone: "needs-setup" };
}

type OverviewCardProps = {
  title: string;
  status?: CardStatus;
  /**
   * While the card's data is in flight, render a skeleton pill where the status
   * label will land so the header doesn't reflow when it resolves.
   */
  statusLoading?: boolean;
  description: string;
  icon: ComponentType<SVGProps<SVGSVGElement>>;
  children?: ReactNode;
  /** Rendered top-right, aligned with the title (e.g. a Manage/View affordance). */
  headerAction?: ReactNode;
};

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
      <EarlyAccessBanner />
      <div className="space-y-5">
        {mcpServer ? (
          <OverviewCards
            mcpServer={mcpServer}
            endpoints={endpoints}
            isLoadingEndpoints={isLoadingEndpoints}
            onShowEndpoints={onShowEndpoints}
            onShowAuthentication={onShowAuthentication}
          />
        ) : (
          <OverviewCardsSkeleton />
        )}
      </div>
      <EnhanceSection />
    </div>
  );
}

function OverviewCards({
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
  return (
    <>
      <ServerAddressCard
        endpoints={endpoints}
        isLoading={isLoadingEndpoints}
        onManage={onShowEndpoints}
      />
      <AuthenticationOverviewCard
        mcpServer={mcpServer}
        onConfigure={onShowAuthentication}
      />
      {mcpServer.remoteMcpServerId ? (
        <SourceOverviewCard remoteMcpServerId={mcpServer.remoteMcpServerId} />
      ) : (
        // /x/mcp only renders mcp_servers-backed (remote MCP) servers, which
        // always carry a remoteMcpServerId, so this branch is currently
        // unreachable. Kept for when toolset-backed servers migrate here
        // (AGE-1902).
        <ToolsOverviewCard toolsetId={mcpServer.toolsetId} />
      )}
    </>
  );
}

function OverviewCardsSkeleton() {
  return (
    <>
      <OverviewCardSkeleton bodyLines={2} />
      <OverviewCardSkeleton bodyLines={1} />
      <OverviewCardSkeleton bodyLines={2} />
    </>
  );
}

function OverviewCardSkeleton({ bodyLines = 2 }: { bodyLines?: number }) {
  return (
    <section className={CARD_SHELL}>
      <Skeleton className="h-12 w-12 shrink-0 rounded-lg" />
      <div className="min-w-0 flex-1 space-y-3">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0 space-y-2">
            <div className="flex items-center gap-2.5">
              <Skeleton className="h-5 w-32" />
              <Skeleton className="h-3.5 w-16 rounded" />
            </div>
            <Skeleton className="h-4 w-72 max-w-full" />
          </div>
          <Skeleton className="h-9 w-24 shrink-0 rounded-md" />
        </div>
        <div className="space-y-2">
          {Array.from({ length: bodyLines }).map((_, i) => (
            <Skeleton key={i} className="h-11 w-full" />
          ))}
        </div>
      </div>
    </section>
  );
}

function UrlFieldSkeleton() {
  return (
    <div className="space-y-1.5">
      <Skeleton className="h-3 w-40" />
      <Skeleton className="h-11 w-full" />
    </div>
  );
}

function EarlyAccessBanner() {
  return (
    <section className={CARD_SHELL}>
      <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg bg-violet-100 dark:bg-violet-950/50">
        <Sparkles className="h-6 w-6 text-violet-600 dark:text-violet-300" />
      </div>
      <div className="min-w-0 flex-1 space-y-1">
        <Type variant="subheading" as="h3">
          Remote MCP is in early access
        </Type>
        <Type muted small>
          This feature is under active development and may change frequently.
          We'd love your feedback:{" "}
          <a
            href="mailto:gram@speakeasy.com?subject=Remote%20MCP%20Feedback"
            className="text-foreground font-medium underline underline-offset-2"
          >
            reach out to the Gram team
          </a>{" "}
          if you run into issues or have questions.
        </Type>
      </div>
    </section>
  );
}

function ServerAddressCard({
  endpoints,
  isLoading,
  onManage,
}: {
  endpoints: McpEndpoint[];
  isLoading: boolean;
  onManage: () => void;
}) {
  // Custom-domain endpoints are the customer-facing URL, so prefer them over
  // the platform-hosted fallback when picking which address to display.
  const endpoint = useMemo(
    () => endpoints.find((e) => e.customDomainId) ?? endpoints[0],
    [endpoints],
  );

  // useMcpEndpointUrl returns undefined while a custom domain is still
  // resolving. This card intentionally ignores verification state, so fall back
  // to the platform URL built from the slug; the hook swaps in the custom-domain
  // URL once it resolves.
  const { mcpUrl: resolvedUrl } = useMcpEndpointUrl(endpoint);
  const mcpUrl =
    resolvedUrl ??
    (endpoint?.slug ? `${getServerURL()}/mcp/${endpoint.slug}` : undefined);
  const installPageUrl = mcpUrl ? `${mcpUrl}/install` : undefined;
  const configured = !!mcpUrl;

  return (
    <OverviewCard
      title="Server Address"
      status={readyStatus(isLoading, configured)}
      statusLoading={isLoading}
      description="This is where clients connect to your server. Add /install to the end to open a guided setup page you can share with users."
      icon={GlobeIcon}
      headerAction={
        <Button variant="secondary" onClick={onManage}>
          <Button.Text>Manage</Button.Text>
        </Button>
      }
    >
      {isLoading ? (
        <div className="space-y-4">
          <UrlFieldSkeleton />
          <UrlFieldSkeleton />
        </div>
      ) : !configured ? (
        <StatusCallout tone="warning" icon={AlertTriangle}>
          A server address must be configured
        </StatusCallout>
      ) : (
        <div className="space-y-4">
          <UrlField
            label="MCP Server URL"
            hint="Clients connect here"
            url={mcpUrl}
          />
          {installPageUrl && (
            <UrlField
              label="Install Page URL"
              hint="Share with your users"
              url={installPageUrl}
            />
          )}
        </div>
      )}
    </OverviewCard>
  );
}

function UrlField({
  label,
  hint,
  url,
}: {
  label: string;
  hint: string;
  url: string;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="text-muted-foreground font-mono font-semibold tracking-wide uppercase">
          {label}
        </span>
        <span className="text-muted-foreground">· {hint}</span>
      </div>
      <CodeBlock innerClassName="bg-muted/40 p-3 pr-12">{url}</CodeBlock>
    </div>
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

const AUTH_COPY: Record<AuthState, string> = {
  "gram-only":
    "Only members of this Speakeasy organization who have been given the appropriate roles & permissions are able to connect to this server.",
  "gram-remote":
    "Members of this Speakeasy organization who have been given the appropriate roles & permissions are able to connect to this server. They will also be required to log into the upstream service.",
  "remote-only":
    "Users are not required to be Speakeasy organization members, and roles & permissions will not apply. They will be required to log into the upstream service before connecting to the server.",
  none: "Anyone can connect to and use this MCP server.",
};

function AuthenticationOverviewCard({
  mcpServer,
  onConfigure,
}: {
  mcpServer: McpServer;
  onConfigure: () => void;
}) {
  const userSessionIssuerId = mcpServer.userSessionIssuerId;
  const { data: clientsResult, isLoading } = useRemoteSessionClients(
    { userSessionIssuerId },
    undefined,
    { enabled: !!userSessionIssuerId },
  );

  const isPublic = mcpServer.visibility === "public";
  const hasIssuer = !!userSessionIssuerId;
  // Whether any upstream identity provider is paired with this issuer.
  const hasRemote = (clientsResult?.result.items.length ?? 0) > 0;
  // Private + issuer means Gram OAuth gates org membership and RBAC.
  const gramGated = hasIssuer && !isPublic;

  let state: AuthState;
  if (gramGated && !hasRemote) state = "gram-only";
  else if (gramGated && hasRemote) state = "gram-remote";
  else if (isPublic && hasRemote) state = "remote-only";
  else state = "none";

  const secure = state !== "none";
  // The remote-identity query only runs when an issuer exists, so gate the
  // loading state on hasIssuer too.
  const loading = hasIssuer && isLoading;

  return (
    <OverviewCard
      title="Authentication"
      status={readyStatus(loading, secure)}
      statusLoading={loading}
      description="Verify who is allowed to connect before traffic flows through."
      icon={secure ? ShieldCheck : Shield}
      headerAction={
        <Button
          variant={secure ? "secondary" : "primary"}
          onClick={onConfigure}
        >
          <Button.Text>{secure ? "Manage" : "Configure"}</Button.Text>
        </Button>
      }
    >
      {loading ? (
        <Skeleton className="h-12 w-full rounded-md" />
      ) : secure ? (
        <StatusCallout tone="ready" icon={ShieldCheck}>
          {AUTH_COPY[state]}
        </StatusCallout>
      ) : (
        <StatusCallout tone="warning" icon={AlertTriangle}>
          {AUTH_COPY[state]}
        </StatusCallout>
      )}
    </OverviewCard>
  );
}

function SourceOverviewCard({
  remoteMcpServerId,
}: {
  remoteMcpServerId: string;
}) {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { data: remoteMcpServer, isLoading } = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { throwOnError: false },
  );

  const ready = !isLoading && !!remoteMcpServer;
  const trimmedName = remoteMcpServer?.name?.trim();

  return (
    <OverviewCard
      title="Source"
      status={ready ? { label: "Ready", tone: "ready" } : undefined}
      statusLoading={isLoading}
      description="The remote MCP server this server proxies."
      icon={Network}
      headerAction={
        remoteMcpServer ? (
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
            <Button.Text>View</Button.Text>
            <Button.RightIcon>
              <ArrowUpRight className="size-4" />
            </Button.RightIcon>
          </Button>
        ) : undefined
      }
    >
      {isLoading || !remoteMcpServer ? (
        <div className="space-y-4">
          <Skeleton className="h-5 w-44" />
          <UrlFieldSkeleton />
        </div>
      ) : (
        <div className="space-y-4">
          {trimmedName && <Type className="font-medium">{trimmedName}</Type>}
          <UrlField
            label="Upstream URL"
            hint="Requests are forwarded here"
            url={remoteMcpServer.url}
          />
        </div>
      )}
    </OverviewCard>
  );
}

function ToolsOverviewCard({ toolsetId }: { toolsetId: string | undefined }) {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { data: toolsetsResult, isLoading } = useListToolsets();
  const toolset = toolsetsResult?.toolsets.find(
    (t: ToolsetEntry) => t.id === toolsetId,
  );

  const ready = !isLoading && !!toolset;
  const displayName = toolset?.name?.trim() || toolset?.slug;

  return (
    <OverviewCard
      title="Tools"
      status={ready ? { label: "Ready", tone: "ready" } : undefined}
      statusLoading={isLoading}
      description="The capabilities and resources exposed to connecting clients."
      icon={Wrench}
      headerAction={
        toolset ? (
          <Button
            variant="secondary"
            onClick={() => navigate(routes.mcp.details.href(toolset.slug))}
          >
            <Button.Text>View</Button.Text>
            <Button.RightIcon>
              <ArrowUpRight className="size-4" />
            </Button.RightIcon>
          </Button>
        ) : undefined
      {isLoading ? (
        <Type muted small>
          Loading tools…
        </Type>
      ) : !toolset ? (
        <StatusCallout tone="warning" icon={AlertTriangle}>
          Couldn't load the linked toolset.
        </StatusCallout>
      ) : (
        displayName && <Type className="font-medium">{displayName}</Type>
      )}
    </OverviewCard>
  );
}

function OverviewCard({
  title,
  status,
  statusLoading,
  description,
  icon: Icon,
  children,
  headerAction,
}: OverviewCardProps) {
  // Amber only flags an actionable gap ("Needs Setup"); ready and in-flight
  // states both read as green so the card doesn't flicker amber while loading.
  const tone = ICON_TONES[status?.tone === "needs-setup" ? "amber" : "green"];

  let statusNode: ReactNode = null;
  if (status) {
    statusNode = <StatusLabel label={status.label} tone={status.tone} />;
  } else if (statusLoading) {
    statusNode = <Skeleton className="h-3.5 w-16 rounded" />;
  }

  return (
    <section className={CARD_SHELL}>
      <div
        className={`flex h-12 w-12 shrink-0 items-center justify-center rounded-lg ${tone.frame}`}
      >
        <Icon className={`h-6 w-6 ${tone.icon}`} />
      </div>
      <div className="min-w-0 flex-1 space-y-3">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="mb-1 flex flex-wrap items-center gap-2.5">
              <Type variant="subheading" as="h3">
                {title}
              </Type>
              {statusNode}
            </div>
            <Type muted small>
              {description}
            </Type>
          </div>
          {headerAction ? <div className="shrink-0">{headerAction}</div> : null}
        </div>
        {children}
      </div>
    </section>
  );
}

function StatusLabel({ label, tone }: { label: string; tone: StatusTone }) {
  const colorClassName =
    tone === "ready"
      ? "text-emerald-700 dark:text-emerald-300"
      : "text-amber-600 dark:text-amber-300";

  return (
    <span
      className={`font-mono text-[11px] font-semibold tracking-wide uppercase ${colorClassName}`}
    >
      {label}
    </span>
  );
}

function EnhanceSection() {
  return (
    <section>
      <Type
        muted
        small
        className="font-mono text-xs tracking-[0.2em] uppercase"
      >
        Optional
      </Type>
      <Heading variant="h3" className="mt-1 mb-1 font-semibold normal-case">
        Enhance your server
      </Heading>
      <Type muted small className="mb-5">
        Add-ons that improve the experience. They won't block enabling.
      </Type>
      <EnhancementCard
        icon={BookOpen}
        title="Install page"
        description="Give users a branded, one-click page with setup instructions for connecting."
        meta="Default"
        actionLabel="Customize"
        comingSoon
      />
    </section>
  );
}

function EnhancementCard({
  icon: Icon,
  title,
  description,
  meta,
  actionLabel,
  onAction,
  comingSoon = false,
}: {
  icon: ComponentType<SVGProps<SVGSVGElement>>;
  title: string;
  description: string;
  meta: string;
  actionLabel: string;
  onAction?: () => void;
  comingSoon?: boolean;
}) {
  return (
    <section className="border-border bg-card flex min-h-[176px] flex-col rounded-lg border p-5 shadow-sm">
      <div className="mb-5 flex items-center gap-3">
        <div className="bg-muted flex h-10 w-10 shrink-0 items-center justify-center rounded-md">
          <Icon className="text-muted-foreground h-5 w-5" />
        </div>
        <Type variant="subheading" as="h3">
          {title}
        </Type>
        {comingSoon && (
          <Badge size="sm" variant="neutral" background>
            <Badge.Text>Coming Soon</Badge.Text>
          </Badge>
        )}
      </div>

      <Type muted small className="mb-5">
        {description}
      </Type>

      <div className="mt-auto flex items-center justify-between gap-4">
        <Type
          muted
          small
          className="font-mono text-xs tracking-[0.2em] uppercase"
        >
          {meta}
        </Type>
        {comingSoon ? (
          // A disabled button swallows pointer events, so wrap it in a span
          // for the tooltip trigger to register hover.
          <SimpleTooltip tooltip="Coming Soon">
            <span tabIndex={0}>
              <Button variant="secondary" disabled>
                <Button.Text>{actionLabel}</Button.Text>
              </Button>
            </span>
          </SimpleTooltip>
        ) : (
          <Button variant="secondary" onClick={onAction}>
            <Button.Text>{actionLabel}</Button.Text>
          </Button>
        )}
      </div>
    </section>
  );
}

/** Shared outer chrome for every overview status card (and its skeleton). */
const CARD_SHELL =
  "border-border bg-card flex gap-4 rounded-lg border p-5 shadow-sm";

const ICON_TONES = {
  green: {
    frame: "bg-emerald-100 dark:bg-emerald-950/50",
    icon: "text-emerald-700 dark:text-emerald-300",
  },
  amber: {
    frame: "bg-amber-100 dark:bg-amber-950/50",
    icon: "text-amber-600 dark:text-amber-300",
  },
} as const;

function StatusCallout({
  tone,
  icon: Icon,
  children,
}: {
  tone: "ready" | "warning";
  icon: ComponentType<SVGProps<SVGSVGElement>>;
  children: ReactNode;
}) {
  const styles =
    tone === "ready"
      ? {
          box: "bg-emerald-50 dark:bg-emerald-950/40",
          icon: "text-emerald-600 dark:text-emerald-300",
          text: "text-emerald-700 dark:text-emerald-300",
        }
      : {
          box: "bg-amber-50 dark:bg-amber-950/40",
          icon: "text-amber-600 dark:text-amber-300",
          text: "text-amber-700 dark:text-amber-300",
        };

  return (
    <div
      className={`flex items-start gap-2 rounded-md px-3 py-2 ${styles.box}`}
    >
      <Icon className={`mt-0.5 size-4 shrink-0 ${styles.icon}`} />
      <Type small className={styles.text}>
        {children}
      </Type>
    </div>
  );
}
