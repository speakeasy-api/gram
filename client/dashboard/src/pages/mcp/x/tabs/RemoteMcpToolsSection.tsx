import { extractToolParameters } from "@/components/mcp/tool-parameters";
import {
  McpToolParameters,
  McpToolRow,
  McpToolRows,
  McpToolRowsSkeleton,
} from "@/components/mcp/tool-browser";
import { Heading } from "@/components/ui/Heading";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { type ProxiedMcpTool } from "@/hooks/useProxiedMcpTools";
import { handleError, toError } from "@/lib/errors";
import type { ToolMetadata } from "@gram/client/models/components/toolmetadata.js";
import { ToolAnnotationIndicators } from "./ToolAnnotationIndicators";
import { ToolMetadataDriftPanel } from "./ToolMetadataDriftPanel";
import { computeDrift } from "./toolMetadataSync";
import { useRemoteMcpToolConnection } from "./useRemoteMcpToolConnection";
import { type ToolMetadataByName } from "@/hooks/useToolMetadata";
import { Button } from "@/components/ui/Button";
import { QueryErrorResetBoundary } from "@tanstack/react-query";
import { PlugZap } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { ErrorBoundary, type FallbackProps } from "react-error-boundary";
import { Link } from "react-router";

type RemoteMcpToolsSectionProps = {
  /** The Speakeasy-proxied MCP URL to connect to; undefined while endpoints load. */
  mcpUrl: string | undefined;
  /** True while the server address / endpoints are still resolving. */
  isResolvingUrl: boolean;
  /** The mcp_server id, used to mint the user-session JWT. */
  mcpServerId: string | undefined;
  /**
   * The server's user_session_issuer id, if it has one. Doubles as the
   * issuer-gated flag and keys the minted JWT, so relinking the server to a
   * different issuer discards the token bound to the old one.
   */
  userSessionIssuerId: string | undefined;
  /**
   * The backing remote_mcp_server id, when there is one. Only remote-backed
   * servers carry tool metadata — the API rejects toolset-backed ones.
   */
  remoteMcpServerId?: string;
  /** Disabled servers cannot serve MCP requests. */
  isDisabled: boolean;
  /**
   * Deep link to the server's Authentication settings section. Surfaced when a
   * server with no authentication configured fails to list its tools.
   */
  authSettingsHref?: string;
  /**
   * Speakeasy-origin endpoint slug. Custom-domain slugs are a different namespace
   * and must not be used to build the first-party connect URL.
   */
  platformSlug?: string;
  /** Set when the backend is a tunnel. Public tunnels have no Connect route. */
  tunneledMcpServerId?: string;
  visibility?: string;
};

/**
 * Lists the tools advertised by the remote MCP server, connecting through the
 * Speakeasy-proxied `/mcp/<slug>` endpoint via the AI SDK MCP client.
 *
 * For issuer-gated servers we mint a user-session JWT scoped to the mcp_server
 * and connect with it. When no upstream remote_session exists yet the gateway
 * 401s into `needsAuth`, and we surface a Connect button that opens the
 * first-party connect page in a new tab; returning focus re-attempts the list.
 * That page 404s unless the server is issuer-gated on a Speakeasy-hosted address, so
 * a 401 from any other server points at authentication settings instead.
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
              isIssuerGated={!!props.userSessionIssuerId}
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
      <Text muted small className="mb-5">
        Tools exposed by this MCP server.
      </Text>
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
        <ConfigureAuthenticationState
          message={unconfiguredAuthMessage}
          authSettingsHref={authSettingsHref}
          onRetry={resetErrorBoundary}
        />
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

const unconfiguredAuthMessage =
  "This server has no authentication configured yet. Set up an authorization server so its tools can be listed.";

const unavailableConnectMessage =
  "Connect isn't available for this server. Review its authentication settings.";

function RemoteMcpToolsSectionInner({
  mcpUrl,
  isResolvingUrl,
  mcpServerId,
  userSessionIssuerId,
  remoteMcpServerId,
  authSettingsHref,
  platformSlug,
  tunneledMcpServerId,
  visibility,
}: RemoteMcpToolsSectionProps): JSX.Element {
  const {
    tools,
    metadataByTool,
    loading: connectionLoading,
    needsAuth,
    isError,
    isIssuerGated,
    refetch,
    connect,
    sync,
    isSyncing,
  } = useRemoteMcpToolConnection({
    mcpUrl,
    mcpServerId,
    userSessionIssuerId,
    remoteMcpServerId,
    platformSlug,
    tunneledMcpServerId,
    visibility,
  });

  const toolEntries = useMemo(
    () => (tools ? Object.entries(tools) : []),
    [tools],
  );

  const loading = isResolvingUrl || connectionLoading;
  const tracksMetadata = !!remoteMcpServerId;

  const drift = useMemo(
    () => (tracksMetadata && tools ? computeDrift(tools, metadataByTool) : []),
    [tracksMetadata, tools, metadataByTool],
  );

  return (
    <ToolsSectionShell>
      {!loading && drift.length > 0 ? (
        <ToolMetadataDriftPanel
          drift={drift}
          mcpServerId={mcpServerId}
          onSync={sync}
          isSyncing={isSyncing}
        />
      ) : null}
      <RemoteMcpToolsBody
        loading={loading}
        needsAuth={needsAuth}
        isError={isError}
        isIssuerGated={isIssuerGated}
        authSettingsHref={authSettingsHref}
        toolEntries={toolEntries}
        metadataByTool={metadataByTool}
        onRetry={refetch}
        onConnect={connect}
      />
    </ToolsSectionShell>
  );
}

function RemoteMcpToolsBody({
  loading,
  needsAuth,
  isError,
  isIssuerGated,
  authSettingsHref,
  toolEntries,
  metadataByTool,
  onRetry,
  onConnect,
}: {
  loading: boolean;
  needsAuth: boolean;
  isError: boolean;
  isIssuerGated: boolean;
  authSettingsHref?: string;
  toolEntries: Array<[string, ProxiedMcpTool]>;
  metadataByTool: ToolMetadataByName;
  onRetry: () => void;
  onConnect?: () => void;
}): JSX.Element {
  if (loading) {
    return <McpToolRowsSkeleton />;
  }

  if (needsAuth && onConnect) {
    return <RemoteMcpToolsConnectPrompt onConnect={onConnect} />;
  }

  // A 401 with no connect target used to open the first-party connect route
  // anyway, which returns a bare not_found document. Point at settings instead.
  if (needsAuth) {
    return (
      <ConfigureAuthenticationState
        message={
          isIssuerGated ? unavailableConnectMessage : unconfiguredAuthMessage
        }
        authSettingsHref={authSettingsHref}
        onRetry={onRetry}
      />
    );
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

  return (
    <RemoteMcpToolsList
      toolEntries={toolEntries}
      metadataByTool={metadataByTool}
    />
  );
}

/**
 * The tool list (styled to match the toolset Tools tab) plus a details drawer.
 * Selecting a row opens a right-side Sheet with the tool's full description and
 * parameters; closing it clears the selection.
 */
function RemoteMcpToolsList({
  toolEntries,
  metadataByTool,
}: {
  toolEntries: Array<[string, ProxiedMcpTool]>;
  metadataByTool: ToolMetadataByName;
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
      <McpToolRows>
        {toolEntries.map(([name, tool]) => (
          <McpToolRow
            key={name}
            name={name}
            description={tool.description}
            hint={
              <ToolAnnotationIndicators
                annotations={tool.annotations}
                stored={metadataByTool[name]}
              />
            }
            selected={name === selectedName}
            onSelect={() => setSelectedName(name)}
          />
        ))}
      </McpToolRows>

      <Sheet
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelectedName(null);
        }}
      >
        <SheetContent className="w-full gap-0 sm:max-w-md">
          {selected && (
            <RemoteToolDetails
              name={selected[0]}
              tool={selected[1]}
              stored={metadataByTool[selected[0]]}
            />
          )}
        </SheetContent>
      </Sheet>
    </>
  );
}

/** The drawer body: the selected tool's full description and parameters. */
function RemoteToolDetails({
  name,
  tool,
  stored,
}: {
  name: string;
  tool: ProxiedMcpTool;
  stored?: ToolMetadata;
}): JSX.Element {
  const parameters = useMemo(
    () => extractToolParameters(tool.inputSchema),
    [tool.inputSchema],
  );

  return (
    <>
      <SheetHeader className="gap-2 border-b">
        <div className="flex items-center gap-2 pr-8">
          <SheetTitle className="font-mono text-sm break-all">
            {name}
          </SheetTitle>
          <ToolAnnotationIndicators
            annotations={tool.annotations}
            stored={stored}
          />
        </div>
        {(stored?.title ?? tool.annotations?.title) ? (
          <Text muted small as="p">
            {stored?.title ?? tool.annotations?.title}
          </Text>
        ) : null}
        <SheetDescription className="whitespace-pre-line">
          {tool.description || "No description provided."}
        </SheetDescription>
      </SheetHeader>

      <div className="flex-1 overflow-y-auto p-4">
        <Text variant="subheading" as="h4" className="mb-3">
          Parameters
        </Text>
        <McpToolParameters parameters={parameters} />
      </div>
    </>
  );
}

function ConfigureAuthenticationState({
  message,
  authSettingsHref,
  onRetry,
}: {
  message: string;
  authSettingsHref?: string;
  onRetry?: () => void;
}): JSX.Element {
  return (
    <EmptyState message={message} onRetry={onRetry}>
      {authSettingsHref ? (
        <Button variant="secondary" asChild>
          <Link to={authSettingsHref}>
            <Button.Text>Configure authentication</Button.Text>
          </Link>
        </Button>
      ) : null}
    </EmptyState>
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
    <div className="border-border flex flex-col items-start gap-2 border border-dashed px-4 py-6">
      <Text muted small>
        {message}
      </Text>
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
    <div className="border-neutral-softest flex flex-col items-center gap-3 border px-6 py-12 text-center">
      <PlugZap className="text-muted-foreground/70 size-8" />
      <Text muted small>
        Connect to this MCP to view the tools.
      </Text>
      {onConnect ? (
        <Button variant="secondary" onClick={onConnect}>
          <Button.Text>Connect</Button.Text>
        </Button>
      ) : null}
    </div>
  );
}
