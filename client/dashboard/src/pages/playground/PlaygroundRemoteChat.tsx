import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { PlugZap } from "lucide-react";
import { useEffect } from "react";
import { PlaygroundChat } from "./PlaygroundChat";
import { useRemoteMcpConnection } from "./useRemoteMcpConnection";

interface PlaygroundRemoteChatProps {
  mcpServerId: string;
  isIssuerGated: boolean;
  environmentSlug: string | null;
  model: string;
  additionalActions?: React.ReactNode;
}

/**
 * The remote-MCP-backed playground variant: resolves the server's proxied
 * `/mcp/<slug>` URL and issuer-gated token, prompts for an upstream connect
 * when needed, then renders the shared {@link PlaygroundChat}. Environment and
 * MCP-App registration don't apply to remote servers, so both are omitted.
 */
export function PlaygroundRemoteChat({
  mcpServerId,
  isIssuerGated,
  environmentSlug,
  model,
  additionalActions,
}: PlaygroundRemoteChatProps): JSX.Element {
  const {
    mcpUrl,
    gatewayToken,
    needsAuth,
    isError,
    connectUrl,
    refetch,
    isLoading,
    connectionReady,
  } = useRemoteMcpConnection(mcpServerId, isIssuerGated);

  // When the user comes back from the connect tab, re-attempt the connection so
  // a freshly linked session surfaces without a manual refresh.
  useEffect(() => {
    if (!needsAuth) return;
    const onFocus = () => refetch();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [needsAuth, refetch]);

  if (needsAuth) {
    return <RemoteConnectPrompt connectUrl={connectUrl} />;
  }

  if (isError) {
    return (
      <RemoteStatusNotice message="Couldn't connect to this MCP server to list its tools.">
        <Button variant="secondary" onClick={refetch}>
          Try again
        </Button>
      </RemoteStatusNotice>
    );
  }

  if (!mcpUrl || isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Type muted>Connecting to MCP server…</Type>
      </div>
    );
  }

  // Issuer-gated server whose JWT never arrived (mint failed): block rather than
  // open an unauthenticated chat that would 401 on every request.
  if (!connectionReady) {
    return (
      <RemoteStatusNotice message="Couldn't authenticate with this MCP server. Check the server's identity provider configuration." />
    );
  }

  return (
    <PlaygroundChat
      mcpUrl={mcpUrl}
      gatewayToken={gatewayToken}
      model={model}
      environmentSlug={environmentSlug}
      additionalActions={additionalActions}
    />
  );
}

function RemoteStatusNotice({
  message,
  children,
}: {
  message: string;
  children?: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="border-neutral-softest flex max-w-md flex-col items-center gap-3 rounded-lg border px-6 py-12 text-center">
        <PlugZap className="text-muted-foreground/70 size-8" />
        <Type muted className="text-sm">
          {message}
        </Type>
        {children}
      </div>
    </div>
  );
}

function RemoteConnectPrompt({
  connectUrl,
}: {
  connectUrl: string | undefined;
}): JSX.Element {
  const handleConnect = () => {
    if (connectUrl) window.open(connectUrl, "_blank", "noopener,noreferrer");
  };

  return (
    <div className="flex h-full items-center justify-center">
      <div className="border-neutral-softest flex max-w-md flex-col items-center gap-3 rounded-lg border px-6 py-12 text-center">
        <PlugZap className="text-muted-foreground/70 size-8" />
        <Type className="font-medium">Connection Required</Type>
        <Type muted className="text-sm">
          Connect to this MCP server to authorize access before chatting with
          its tools.
        </Type>
        {connectUrl ? (
          <Button variant="secondary" onClick={handleConnect}>
            Connect
          </Button>
        ) : null}
      </div>
    </div>
  );
}
