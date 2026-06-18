import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useRemoteMcpTools } from "@/hooks/useRemoteMcpTools";
import { useRemoteMcpUserSessionToken } from "@/hooks/useRemoteMcpUserSessionToken";
import { handleError } from "@/lib/errors";
import { mcpConnectionUrl } from "@/lib/utils";
import { QueryErrorResetBoundary } from "@tanstack/react-query";
import { useMemo, type ReactNode } from "react";
import { ErrorBoundary, type FallbackProps } from "react-error-boundary";

type RemoteMcpToolsSectionProps = {
  /** The Gram-proxied MCP URL to connect to; undefined while endpoints load. */
  mcpUrl: string | undefined;
  /** True while the server address / endpoints are still resolving. */
  isResolvingUrl: boolean;
  /** The mcp_server id, used to mint the user-session JWT. */
  mcpServerId: string | undefined;
  /** Whether the server is issuer-gated (has a user_session_issuer). */
  isIssuerGated: boolean;
};

/**
 * Lists the tools advertised by the remote MCP server, connecting through the
 * Gram-proxied `/mcp/<slug>` endpoint via the AI SDK MCP client.
 *
 * For issuer-gated servers we mint a user-session JWT scoped to the mcp_server
 * and connect with it; without one the connection 401s into `needsAuth` (the
 * Authenticate flow lands in a later increment).
 *
 * Expected fetch failures are rendered inline (see RemoteMcpToolsBody). The
 * surrounding ErrorBoundary is the defensive layer for unexpected render-time
 * throws — its Retry resets the boundary and any errored queries so a fresh
 * attempt runs without reloading the page.
 */
export function RemoteMcpToolsSection(
  props: RemoteMcpToolsSectionProps,
): JSX.Element {
  return (
    <QueryErrorResetBoundary>
      {({ reset }) => (
        <ErrorBoundary
          onReset={reset}
          FallbackComponent={RemoteMcpToolsErrorFallback}
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
        Tools exposed by this remote MCP server.
      </Type>
      {children}
    </section>
  );
}

function RemoteMcpToolsErrorFallback({
  error,
  resetErrorBoundary,
}: FallbackProps): JSX.Element {
  handleError(error, { silent: true });

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
    useRemoteMcpUserSessionToken({ mcpServerId, isIssuerGated });

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

  const { tools, isLoading, needsAuth, isError, refetch } = useRemoteMcpTools(
    connectUrl,
    { headers, enabled: connectionEnabled },
  );

  const toolEntries = useMemo(
    () => (tools ? Object.entries(tools) : []),
    [tools],
  );

  const loading = isResolvingUrl || isTokenLoading || isLoading;

  return (
    <ToolsSectionShell>
      <RemoteMcpToolsBody
        loading={loading}
        needsAuth={needsAuth}
        isError={isError}
        toolEntries={toolEntries}
        onRetry={refetch}
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
}: {
  loading: boolean;
  needsAuth: boolean;
  isError: boolean;
  toolEntries: Array<[string, { description?: string }]>;
  onRetry: () => void;
}): JSX.Element {
  if (loading) {
    return (
      <div className="space-y-2">
        <ToolRowSkeleton />
        <ToolRowSkeleton />
        <ToolRowSkeleton />
      </div>
    );
  }

  if (needsAuth) {
    return (
      <EmptyState message="Authenticate with this server to list its tools." />
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
    <div className="border-border divide-border divide-y rounded-md border">
      {toolEntries.map(([name, tool]) => (
        <div key={name} className="flex flex-col gap-1 px-4 py-3">
          <Type mono small as="div" className="font-medium break-all">
            {name}
          </Type>
          {tool.description ? (
            <Type muted small as="div" className="break-words">
              {tool.description}
            </Type>
          ) : null}
        </div>
      ))}
    </div>
  );
}

function EmptyState({
  message,
  onRetry,
}: {
  message: string;
  onRetry?: () => void;
}): JSX.Element {
  return (
    <div className="border-border flex flex-col items-start gap-2 rounded-md border border-dashed px-4 py-6">
      <Type muted small>
        {message}
      </Type>
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

function ToolRowSkeleton(): JSX.Element {
  return (
    <div className="border-border flex flex-col gap-2 rounded-md border px-4 py-3">
      <Skeleton className="h-4 w-40" />
      <Skeleton className="h-3 w-80 max-w-full" />
    </div>
  );
}
