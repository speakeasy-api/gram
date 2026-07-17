import {
  AnnotationBadgeIcons,
  type ResolvedToolAnnotations,
} from "@/components/tool-list/AnnotationBadges";
import { Heading } from "@/components/ui/heading";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import {
  useProxiedMcpTools,
  type ProxiedMcpTool,
  type ProxiedMcpToolAnnotations,
} from "@/hooks/useProxiedMcpTools";
import { useProxiedMcpUserSessionToken } from "@/hooks/useProxiedMcpUserSessionToken";
import { handleError, toError } from "@/lib/errors";
import { cn, firstPartyConnectUrl, mcpConnectionUrl } from "@/lib/utils";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { QueryErrorResetBoundary } from "@tanstack/react-query";
import { PlugZap } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { ErrorBoundary, type FallbackProps } from "react-error-boundary";
import { Link } from "react-router";

type RemoteMcpToolsSectionProps = {
  /** The Gram-proxied MCP URL to connect to; undefined while endpoints load. */
  mcpUrl: string | undefined;
  /** True while the server address / endpoints are still resolving. */
  isResolvingUrl: boolean;
  /** The mcp_server id, used to mint the user-session JWT. */
  mcpServerId: string | undefined;
  /** Whether the server is issuer-gated (has a user_session_issuer). */
  isIssuerGated: boolean;
  /** Disabled servers cannot serve MCP requests. */
  isDisabled: boolean;
  /**
   * Deep link to the server's Authentication settings section. Surfaced when a
   * server with no authentication configured fails to list its tools.
   */
  authSettingsHref?: string;
};

/**
 * Lists the tools advertised by the remote MCP server, connecting through the
 * Gram-proxied `/mcp/<slug>` endpoint via the AI SDK MCP client.
 *
 * For issuer-gated servers we mint a user-session JWT scoped to the mcp_server
 * and connect with it. When no upstream remote_session exists yet the gateway
 * 401s into `needsAuth`, and we surface an Authenticate button that opens the
 * first-party connect page in a new tab; returning focus re-attempts the list.
 *
 * Expected fetch failures are rendered inline (see RemoteMcpToolsBody). The
 * surrounding ErrorBoundary is the defensive layer for unexpected render-time
 * throws — its Retry resets the boundary and any errored queries so a fresh
 * attempt runs without reloading the page.
 */
export function RemoteMcpToolsSection(
  props: RemoteMcpToolsSectionProps,
): JSX.Element {
  if (props.isDisabled) {
    return (
      <ToolsSectionShell>
        <EmptyState message="Enable this server to load its tools." />
      </ToolsSectionShell>
    );
  }

  return (
    <QueryErrorResetBoundary>
      {({ reset }) => (
        <ErrorBoundary
          onReset={reset}
          fallbackRender={(fallbackProps) => (
            <RemoteMcpToolsErrorFallback
              {...fallbackProps}
              isIssuerGated={props.isIssuerGated}
              authSettingsHref={props.authSettingsHref}
            />
          )}
        >
          <RemoteMcpToolsSectionInner {...props} />
        </ErrorBoundary>
      )}
    </QueryErrorResetBoundary>
  );
}

/** Section chrome shared by the loaded content and the error fallback. */
function ToolsSectionShell({ children }: { children: ReactNode }): JSX.Element {
  return (
    <section>
      <Heading variant="h3" className="mt-1 mb-1 font-semibold normal-case">
        Tools
      </Heading>
      <Type muted small className="mb-5">
        Tools exposed by this MCP server.
      </Type>
      {children}
    </section>
  );
}

function RemoteMcpToolsErrorFallback({
  error: rawError,
  resetErrorBoundary,
  isIssuerGated,
  authSettingsHref,
}: FallbackProps & {
  isIssuerGated: boolean;
  authSettingsHref?: string;
}): JSX.Element {
  const error = toError(rawError);
  handleError(error, { silent: true });

  // A server with no authentication configured usually can't list its tools —
  // the connection is rejected upstream before any tools come back. Rather
  // than a generic failure, guide the user to set up authentication first.
  // Retry stays available since a non-issuer-gated server with a public
  // upstream can also land here on a transient failure.
  if (!isIssuerGated) {
    return (
      <ToolsSectionShell>
        <EmptyState
          message="This server has no authentication configured yet. Set up an identity provider so its tools can be listed."
          onRetry={resetErrorBoundary}
        >
          {authSettingsHref ? (
            <Button variant="secondary" asChild>
              <Link to={authSettingsHref}>
                <Button.Text>Configure authentication</Button.Text>
              </Link>
            </Button>
          ) : null}
        </EmptyState>
      </ToolsSectionShell>
    );
  }

  return (
    <ToolsSectionShell>
      <EmptyState
        message="Something went wrong loading tools."
        onRetry={resetErrorBoundary}
      />
    </ToolsSectionShell>
  );
}

function RemoteMcpToolsSectionInner({
  mcpUrl,
  isResolvingUrl,
  mcpServerId,
  isIssuerGated,
}: RemoteMcpToolsSectionProps): JSX.Element {
  const { accessToken, isLoading: isTokenLoading } =
    useProxiedMcpUserSessionToken({ mcpServerId, isIssuerGated });

  // Issuer-gated servers must wait for the JWT before connecting, otherwise the
  // unauthenticated request 401s and caches a spurious `needsAuth`.
  const headers = useMemo(
    () =>
      accessToken ? { Authorization: `Bearer ${accessToken}` } : undefined,
    [accessToken],
  );
  const connectionEnabled = !isIssuerGated || !!accessToken;

  // Connect through the dev proxy origin (same-origin) so the AI SDK transport
  // carries the gram_session cookie and the gateway's proxied SSE response
  // isn't dropped on a cross-origin hop. No-op in prod / for custom domains.
  const connectUrl = useMemo(() => mcpConnectionUrl(mcpUrl), [mcpUrl]);

  const { tools, isLoading, needsAuth, isError, refetch } = useProxiedMcpTools(
    connectUrl,
    { headers, enabled: connectionEnabled },
  );

  const toolEntries = useMemo(
    () => (tools ? Object.entries(tools) : []),
    [tools],
  );

  // The first-party connect page is opened as a top-level new tab, so it rides
  // the gram_session cookie on the backend origin (not the dev proxy).
  const authUrl = useMemo(() => firstPartyConnectUrl(mcpUrl), [mcpUrl]);

  // When the user comes back from the connect tab, re-attempt the listing so a
  // freshly linked session surfaces without a manual refresh.
  useEffect(() => {
    if (!needsAuth) return;
    const onFocus = () => refetch();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [needsAuth, refetch]);

  const handleConnect = () => {
    if (authUrl) window.open(authUrl, "_blank", "noopener,noreferrer");
  };

  const loading = isResolvingUrl || isTokenLoading || isLoading;

  return (
    <ToolsSectionShell>
      <RemoteMcpToolsBody
        loading={loading}
        needsAuth={needsAuth}
        isError={isError}
        toolEntries={toolEntries}
        onRetry={refetch}
        onConnect={authUrl ? handleConnect : undefined}
      />
    </ToolsSectionShell>
  );
}

function RemoteMcpToolsBody({
  loading,
  needsAuth,
  isError,
  toolEntries,
  onRetry,
  onConnect,
}: {
  loading: boolean;
  needsAuth: boolean;
  isError: boolean;
  toolEntries: Array<[string, ProxiedMcpTool]>;
  onRetry: () => void;
  onConnect?: () => void;
}): JSX.Element {
  if (loading) {
    return <ToolsListSkeleton />;
  }

  if (needsAuth) {
    return <RemoteMcpToolsConnectPrompt onConnect={onConnect} />;
  }

  if (isError) {
    return (
      <EmptyState
        message="Couldn't connect to this server to list its tools."
        onRetry={onRetry}
      />
    );
  }

  if (toolEntries.length === 0) {
    return <EmptyState message="This server didn't advertise any tools." />;
  }

  return <RemoteMcpToolsList toolEntries={toolEntries} />;
}

/**
 * The tool list (styled to match the toolset Tools tab) plus a details drawer.
 * Selecting a row opens a right-side Sheet with the tool's full description and
 * parameters; closing it clears the selection.
 */
function RemoteMcpToolsList({
  toolEntries,
}: {
  toolEntries: Array<[string, ProxiedMcpTool]>;
}): JSX.Element {
  const [selectedName, setSelectedName] = useState<string | null>(null);

  // Resolve the selection during render so a refetch that drops the selected
  // tool simply closes the drawer rather than showing a stale row.
  const selected =
    selectedName !== null
      ? (toolEntries.find(([name]) => name === selectedName) ?? null)
      : null;

  return (
    <>
      <div className="border-neutral-softest w-full overflow-hidden rounded-lg border">
        {toolEntries.map(([name, tool]) => (
          <RemoteToolRow
            key={name}
            name={name}
            description={tool.description}
            annotations={tool.annotations}
            selected={name === selectedName}
            onSelect={() => setSelectedName(name)}
          />
        ))}
      </div>

      <Sheet
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelectedName(null);
        }}
      >
        <SheetContent className="w-full gap-0 sm:max-w-md">
          {selected && (
            <RemoteToolDetails name={selected[0]} tool={selected[1]} />
          )}
        </SheetContent>
      </Sheet>
    </>
  );
}

/**
 * A single tool row, styled to match the toolset Tools tab (see ToolList's
 * ToolRow): tool name on top with the description truncated to one line below,
 * and any annotation hints rendered as badges beside the name. Remote MCP tools
 * carry no Gram-side identity, so there are no method/variation badges or action
 * menus — selecting a row opens the details drawer instead.
 */
function RemoteToolRow({
  name,
  description,
  annotations,
  selected,
  onSelect,
}: {
  name: string;
  description?: string;
  annotations?: ProxiedMcpToolAnnotations;
  selected: boolean;
  onSelect: () => void;
}): JSX.Element {
  return (
    <div
      role="button"
      tabIndex={0}
      aria-pressed={selected}
      onClick={onSelect}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect();
        }
      }}
      className={cn(
        "border-neutral-softest hover:bg-muted flex cursor-pointer items-center justify-between border-b py-4 pr-3 pl-4 transition-colors last:border-b-0",
        selected && "bg-muted",
      )}
    >
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex min-w-0 items-center gap-2">
          <p className="text-foreground truncate text-sm leading-6">{name}</p>
          <AnnotationBadgeIcons {...resolveAnnotations(annotations)} />
        </div>
        <p className="text-muted-foreground truncate text-sm leading-6">
          {description || "No description"}
        </p>
      </div>
    </div>
  );
}

/** The drawer body: the selected tool's full description and parameters. */
function RemoteToolDetails({
  name,
  tool,
}: {
  name: string;
  tool: ProxiedMcpTool;
}): JSX.Element {
  const parameters = useMemo(() => extractParameters(tool), [tool]);

  return (
    <>
      <SheetHeader className="gap-2 border-b">
        <div className="flex items-center gap-2 pr-8">
          <SheetTitle className="font-mono text-sm break-all">
            {name}
          </SheetTitle>
          <AnnotationBadgeIcons {...resolveAnnotations(tool.annotations)} />
        </div>
        {tool.annotations?.title ? (
          <Type muted small as="p">
            {tool.annotations.title}
          </Type>
        ) : null}
        <SheetDescription className="whitespace-pre-line">
          {tool.description || "No description provided."}
        </SheetDescription>
      </SheetHeader>

      <div className="flex-1 overflow-y-auto p-4">
        <Type variant="subheading" as="h4" className="mb-3">
          Parameters
        </Type>
        {parameters.length === 0 ? (
          <Type muted small as="p">
            This tool takes no parameters.
          </Type>
        ) : (
          <ul className="divide-border divide-y">
            {parameters.map((parameter) => (
              <ToolParameterRow key={parameter.name} parameter={parameter} />
            ))}
          </ul>
        )}
      </div>
    </>
  );
}

function ToolParameterRow({
  parameter,
}: {
  parameter: ToolParameter;
}): JSX.Element {
  return (
    <li className="flex flex-col gap-1 py-3 first:pt-0 last:pb-0">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <Type mono small as="span" className="font-medium break-all">
          {parameter.name}
        </Type>
        <Type mono small as="span" className="text-muted-foreground">
          {parameter.type}
        </Type>
        {parameter.required ? (
          <Badge variant="neutral" size="sm" background>
            <Badge.Text className="text-[10px] uppercase">Required</Badge.Text>
          </Badge>
        ) : null}
      </div>
      {parameter.description ? (
        <Type muted small as="span" className="break-words">
          {parameter.description}
        </Type>
      ) : null}
    </li>
  );
}

function EmptyState({
  message,
  onRetry,
  children,
}: {
  message: string;
  onRetry?: () => void;
  children?: ReactNode;
}): JSX.Element {
  return (
    <div className="border-border flex flex-col items-start gap-2 rounded-md border border-dashed px-4 py-6">
      <Type muted small>
        {message}
      </Type>
      {children}
      {onRetry ? (
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground text-sm underline"
          onClick={onRetry}
        >
          Try again
        </button>
      ) : null}
    </div>
  );
}

function ToolsListSkeleton(): JSX.Element {
  return (
    <div className="border-neutral-softest w-full overflow-hidden rounded-lg border">
      {Array.from({ length: 5 }).map((_, index) => (
        <ToolRowSkeleton key={index} />
      ))}
    </div>
  );
}

function ToolRowSkeleton(): JSX.Element {
  return (
    <div className="border-neutral-softest flex flex-col gap-2 border-b py-4 pr-3 pl-4 last:border-b-0">
      <Skeleton className="h-4 w-48" />
      <Skeleton className="h-3 w-80 max-w-full" />
    </div>
  );
}

/**
 * The needs-connect state: a centered card prompting the user to connect
 * upstream before tools can be listed. Connecting opens the first-party connect
 * page; returning focus re-attempts the listing.
 */
function RemoteMcpToolsConnectPrompt({
  onConnect,
}: {
  onConnect?: () => void;
}): JSX.Element {
  return (
    <div className="border-neutral-softest flex flex-col items-center gap-3 rounded-lg border px-6 py-12 text-center">
      <PlugZap className="text-muted-foreground/70 size-8" />
      <Type muted small>
        Connect to this MCP to view the tools.
      </Type>
      {onConnect ? (
        <Button variant="secondary" onClick={onConnect}>
          <Button.Text>Connect</Button.Text>
        </Button>
      ) : null}
    </div>
  );
}

/** Map raw MCP annotation hints to the booleans AnnotationBadgeIcons renders. */
function resolveAnnotations(
  annotations: ProxiedMcpToolAnnotations | undefined,
): ResolvedToolAnnotations {
  return {
    readOnly: Boolean(annotations?.readOnlyHint),
    destructive: Boolean(annotations?.destructiveHint),
    idempotent: Boolean(annotations?.idempotentHint),
    openWorld: Boolean(annotations?.openWorldHint),
  };
}

type ToolParameter = {
  name: string;
  type: string;
  description?: string;
  required: boolean;
};

/** Flatten a tool's JSON Schema into a flat list of named parameters. */
function extractParameters(tool: ProxiedMcpTool): ToolParameter[] {
  const schema = readJsonSchema(tool.inputSchema);
  if (!schema) return [];

  const properties = isRecord(schema.properties) ? schema.properties : {};
  const required = new Set(
    Array.isArray(schema.required)
      ? schema.required.filter((r): r is string => typeof r === "string")
      : [],
  );

  return Object.entries(properties).map(([name, def]) => ({
    name,
    type: jsonSchemaTypeLabel(def),
    description:
      isRecord(def) && typeof def.description === "string"
        ? def.description
        : undefined,
    required: required.has(name),
  }));
}

/**
 * The hook hands us the raw MCP `inputSchema` (a JSON Schema object), so the
 * value itself is the schema. Some AI SDK code paths instead wrap it via
 * `jsonSchema()`, stowing the schema on `.jsonSchema` — unwrap that if present.
 */
function readJsonSchema(
  inputSchema: unknown,
): Record<string, unknown> | undefined {
  if (!isRecord(inputSchema)) return undefined;
  if (isRecord(inputSchema.jsonSchema)) return inputSchema.jsonSchema;
  return inputSchema;
}

/** A short, human-readable type label for a JSON Schema property definition. */
function jsonSchemaTypeLabel(def: unknown): string {
  if (!isRecord(def)) return "unknown";

  if (Array.isArray(def.enum) && def.enum.length > 0) {
    return def.enum.map((value) => JSON.stringify(value)).join(" | ");
  }

  const type = def.type;
  if (Array.isArray(type)) {
    const names = type.filter((t): t is string => typeof t === "string");
    return names.length > 0 ? names.join(" | ") : "unknown";
  }
  if (type === "array") {
    const items = def.items;
    const itemType =
      isRecord(items) && typeof items.type === "string"
        ? items.type
        : undefined;
    return itemType ? `${itemType}[]` : "array";
  }

  return typeof type === "string" ? type : "unknown";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
