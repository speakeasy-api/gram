import { PlugZap, TriangleAlert } from "lucide-react";
import { InspectState } from "../../inspect/InspectState";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { handleError, toError } from "@/lib/errors";
import { DEFAULT_MODEL } from "@/lib/models";
import { PlaygroundChat } from "@/pages/playground/PlaygroundChat";
import { getServerURL } from "@/lib/utils";
import { QueryErrorResetBoundary } from "@tanstack/react-query";
import { useMemo, type ReactNode } from "react";
import { ErrorBoundary, type FallbackProps } from "react-error-boundary";
import { Link } from "react-router";
import { InspectWorkbench } from "../../inspect/InspectWorkbench";
import { runMcpTool } from "../../inspect/runMcpTool";
import type { InspectTool } from "../../inspect/types";
import { useToolHealth } from "../../inspect/useToolHealth";
import { DriftNotice } from "../../inspect/DriftNotice";
import { computeDrift } from "./toolMetadataSync";
import { useRemoteMcpToolConnection } from "./useRemoteMcpToolConnection";

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
  /** Where blocked tools' role rules are changed. */
  accessHref?: string;
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
 * Inspect for one MCP server: the tools this viewer's agents get, why any are
 * missing, and a panel to run one or chat with the server.
 *
 * Issuer-gated servers connect with a minted user-session JWT. With no upstream
 * session yet the listing 401s into `needsAuth`, and Connect opens the
 * first-party connect page in a new tab; returning focus lists again. That page
 * 404s unless the server is issuer-gated on a Speakeasy-hosted address, so a 401
 * from any other server points at authentication settings instead.
 *
 * Expected fetch failures render inline. The surrounding ErrorBoundary is the
 * defensive layer for unexpected render-time throws.
 */
export function RemoteMcpToolsSection(
  props: RemoteMcpToolsSectionProps,
): JSX.Element {
  if (props.isDisabled) {
    return (
      <StateNotice message="This server is disabled. Enable it to see and run its tools." />
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

  // A server with no authentication configured usually can't list its tools.
  // Retry stays available since a public upstream can land here on a blip.
  if (!isIssuerGated) {
    return (
      <StateNotice
        message={unconfiguredAuthMessage}
        authSettingsHref={authSettingsHref}
        onRetry={resetErrorBoundary}
      />
    );
  }
  return (
    <StateNotice
      message="Something went wrong loading this server's tools."
      onRetry={resetErrorBoundary}
    />
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
  accessHref,
  platformSlug,
  tunneledMcpServerId,
  visibility,
}: RemoteMcpToolsSectionProps): JSX.Element {
  const connection = useRemoteMcpToolConnection({
    mcpUrl,
    mcpServerId,
    userSessionIssuerId,
    remoteMcpServerId,
    platformSlug,
    tunneledMcpServerId,
    visibility,
    chatSession: true,
  });
  const {
    tools: listed,
    metadataByTool,
    needsAuth,
    isError,
    isIssuerGated,
    refetch,
    connect,
    sync,
    isSyncing,
    canWrite,
    connectUrl,
    headers,
  } = connection;
  const { healthFor } = useToolHealth({ mcpServerId });

  const tools = useMemo<InspectTool[]>(
    () =>
      Object.entries(listed ?? {}).map(([name, tool]) => ({
        name,
        description: tool.description,
        inputSchema: tool.inputSchema,
        annotations: tool.annotations,
        stored: metadataByTool[name],
        unavailable: tool.unavailable
          ? { reason: tool.unavailableReason }
          : undefined,
      })),
    [listed, metadataByTool],
  );

  const loading = isResolvingUrl || connection.loading;

  // Sync is for admins correcting an existing record. An empty record fills in
  // as soon as the server's tools are listed, so there is nothing to offer yet.
  const canOfferSync =
    !!remoteMcpServerId && canWrite && Object.keys(metadataByTool).length > 0;
  const drift = useMemo(
    () => (canOfferSync && listed ? computeDrift(listed, metadataByTool) : []),
    [canOfferSync, listed, metadataByTool],
  );

  const usable = tools.filter((tool) => !tool.unavailable).length;
  const lens = (
    <Text className="text-base">
      What <span className="font-medium">you</span> get from this server
      {!loading && listed ? (
        <span className="text-muted-foreground">
          {" "}
          · {usable} of {tools.length} {tools.length === 1 ? "tool" : "tools"}
        </span>
      ) : null}
    </Text>
  );

  let state: ReactNode;
  if (needsAuth && connect) {
    state = <ConnectPrompt onConnect={connect} />;
  } else if (needsAuth) {
    // A 401 with no connect target used to open the first-party connect route
    // anyway, which returns a bare not_found document. Point at settings.
    state = (
      <StateNotice
        message={
          isIssuerGated ? unavailableConnectMessage : unconfiguredAuthMessage
        }
        authSettingsHref={authSettingsHref}
        onRetry={refetch}
      />
    );
  } else if (isError) {
    state = (
      <StateNotice
        message="Couldn't connect to this server to list its tools."
        onRetry={refetch}
      />
    );
  } else if (listed && tools.length === 0) {
    state = <StateNotice message="This server didn't advertise any tools." />;
  }

  const canChat = !!mcpServerId && !needsAuth && !isError && !!listed;
  // The chat prefers the Speakeasy origin. A server reachable only on a
  // custom domain chats at the address Inspect resolved for it.
  const chatUrl = platformSlug
    ? `${getServerURL()}/mcp/${platformSlug}`
    : mcpUrl;

  return (
    <InspectWorkbench
      lens={lens}
      tools={tools}
      isLoading={loading}
      state={state}
      notice={
        !loading && drift.length > 0 ? (
          <DriftNotice drift={drift} onSync={sync} isSyncing={isSyncing} />
        ) : undefined
      }
      healthFor={healthFor}
      accessHref={accessHref}
      runTool={
        connectUrl
          ? (tool, args) =>
              runMcpTool({ url: connectUrl, headers, name: tool.name, args })
          : undefined
      }
      chat={
        canChat && chatUrl ? (
          // Reuses this tab's connection: the same Speakeasy-origin URL and
          // the same session token, so chatting never asks to connect again.
          <PlaygroundChat
            mcpUrl={chatUrl}
            gatewayToken={headers?.Authorization?.replace(/^Bearer /, "")}
            environmentSlug={null}
            compact
            model={DEFAULT_MODEL}
          />
        ) : undefined
      }
    />
  );
}

/**
 * The needs-connect state. Connecting opens the first-party connect page;
 * returning focus lists again, which saves any tools new to the project, so
 * the copy says so.
 */
function ConnectPrompt({ onConnect }: { onConnect: () => void }): JSX.Element {
  return (
    <InspectState
      icon={PlugZap}
      heading="Sign in to see this server's tools"
      description="Connect your own account, as an agent would. Speakeasy then saves the tool list for this project, so roles can use it."
      action={
        <Button onClick={onConnect}>
          <Button.Text>Connect account</Button.Text>
        </Button>
      }
    />
  );
}

function StateNotice({
  message,
  authSettingsHref,
  onRetry,
}: {
  message: string;
  authSettingsHref?: string;
  onRetry?: () => void;
}): JSX.Element {
  return (
    <InspectState
      icon={TriangleAlert}
      tone="warning"
      heading={message}
      action={
        authSettingsHref || onRetry ? (
          <>
            {authSettingsHref ? (
              <Button asChild>
                <Link to={authSettingsHref}>
                  <Button.Text>Configure authentication</Button.Text>
                </Link>
              </Button>
            ) : null}
            {onRetry ? (
              <Button variant="secondary" onClick={onRetry}>
                <Button.Text>Try again</Button.Text>
              </Button>
            ) : null}
          </>
        ) : undefined
      }
    />
  );
}
