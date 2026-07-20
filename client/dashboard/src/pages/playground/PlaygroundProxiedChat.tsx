import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { PlugZap } from "lucide-react";
import { useEffect } from "react";
import { PlaygroundChat } from "./PlaygroundChat";
import { useProxiedMcpConnection } from "./useProxiedMcpConnection";

interface PlaygroundProxiedChatProps {
  mcpServerId: string;
  isIssuerGated: boolean;
  environmentSlug: string | null;
  model: string;
  additionalActions?: React.ReactNode;
}

/**
 * The proxied-MCP-backed playground variant: resolves the server's proxied
 * `/mcp/<slug>` URL and issuer-gated token, prompts for an upstream connect
 * when needed, then renders the shared {@link PlaygroundChat}. Environment and
 * MCP-App registration don't apply to proxied servers, so both are omitted.
 */
export function PlaygroundProxiedChat({
  mcpServerId,
  isIssuerGated,
  environmentSlug,
  model,
  additionalActions,
}: PlaygroundProxiedChatProps): JSX.Element {
  const {
    mcpUrl,
    gatewayToken,
    needsAuth,
    isError,
    connectUrl,
    refetch,
    isLoading,
    connectionReady,
    needsExplicitConnect,
    requestConnect,
  } = useProxiedMcpConnection(mcpServerId, isIssuerGated);

  // When the user comes back from the connect tab, re-attempt the connection so
  // a freshly linked session surfaces without a manual refresh.
  useEffect(() => {
    if (!needsAuth) return;
    const onFocus = () => refetch();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [needsAuth, refetch]);

  // Issuer-gated servers need an explicit Connect before the playground mints
  // a user-session token (minting persists a session row server-side).
  if (needsExplicitConnect) {
    return <ExplicitConnectPrompt onConnect={requestConnect} />;
  }

  if (needsAuth) {
    return <ProxiedConnectPrompt connectUrl={connectUrl} />;
  }

  if (isError) {
    return (
      <ProxiedStatusNotice message="Couldn't connect to this MCP server to list its tools.">
        <Button variant="secondary" onClick={refetch}>
          Try again
        </Button>
      </ProxiedStatusNotice>
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
      <ProxiedStatusNotice message="Couldn't authenticate with this MCP server. Check the server's identity provider configuration." />
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

function ProxiedStatusNotice({
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

/**
 * The explicit-consent gate for issuer-gated servers: connecting mints a
 * user-session token, which establishes a session on the server, so we wait
 * for a deliberate click instead of minting on page load. Mirrors
 * ConnectRequiredNotice in PlaygroundElements.tsx.
 */
function ExplicitConnectPrompt({
  onConnect,
}: {
  onConnect: () => void;
}): JSX.Element {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="border-neutral-softest flex max-w-md flex-col items-center gap-3 rounded-lg border px-6 py-12 text-center">
        <PlugZap className="text-muted-foreground/70 size-8" />
        <Type className="font-medium">Connect to start chatting</Type>
        <Type muted className="text-sm">
          Connecting to this MCP server establishes a user session for your
          account so the playground can call its tools on your behalf.
        </Type>
        <Button onClick={onConnect}>Connect</Button>
      </div>
    </div>
  );
}

function ProxiedConnectPrompt({
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
