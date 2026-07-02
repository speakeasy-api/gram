import { Type } from "@/components/ui/type";
import { useHasUserConnected } from "@/hooks/useHasUserConnected";
import { useRemoteMcpUserSessionToken } from "@/hooks/useRemoteMcpUserSessionToken";
import { Button } from "@speakeasy-api/moonshine";
import { PlugZap } from "lucide-react";
import { createContext, useContext, useMemo, type ReactNode } from "react";

interface RemoteMcpConnectionValue {
  /** Auth headers for MCP requests; undefined when no credential applies. */
  headers: Record<string, string> | undefined;
  /** Opens the first-party connect page; undefined when the URL is unknown. */
  connect: (() => void) | undefined;
}

const RemoteMcpConnectionContext =
  createContext<RemoteMcpConnectionValue | null>(null);

export function useRemoteMcpConnection(): RemoteMcpConnectionValue {
  const value = useContext(RemoteMcpConnectionContext);
  if (!value) {
    throw new Error(
      "useRemoteMcpConnection must be used within RemoteMcpConnection",
    );
  }
  return value;
}

type RemoteMcpConnectionProps = {
  /** The mcp_server id the user-session JWT is minted against. */
  mcpServerId: string | undefined;
  /** The server's user_session_issuer id; undefined when not issuer-gated. */
  userSessionIssuerId: string | undefined;
  /** The first-party connect page URL (see firstPartyConnectUrl). */
  authUrl: string | undefined;
  /** Rendered while connection state / the JWT is still resolving. */
  fallback: ReactNode;
  children: ReactNode;
};

/**
 * Decides once whether the user can talk to a remote MCP server and tunnels
 * the resulting connection to consumers via useRemoteMcpConnection.
 *
 * Minting a user-session JWT persists a user_sessions row the server operator
 * can see, so viewing a page must never mint. That invariant is structural:
 * the mint (MintedConnection) mounts only behind the hasUserConnected gate,
 * and disconnected users get the Connect prompt with no children rendered.
 */
export function RemoteMcpConnection({
  mcpServerId,
  userSessionIssuerId,
  authUrl,
  fallback,
  children,
}: RemoteMcpConnectionProps): JSX.Element {
  const connect = useMemo(() => {
    if (!authUrl) return undefined;
    return () => window.open(authUrl, "_blank", "noopener,noreferrer");
  }, [authUrl]);

  const anonymous = useMemo<RemoteMcpConnectionValue>(
    () => ({ headers: undefined, connect }),
    [connect],
  );

  if (!userSessionIssuerId) {
    // Not issuer-gated: connect anonymously, no credential to establish.
    return (
      <RemoteMcpConnectionContext.Provider value={anonymous}>
        {children}
      </RemoteMcpConnectionContext.Provider>
    );
  }

  return (
    <GatedConnection
      mcpServerId={mcpServerId}
      userSessionIssuerId={userSessionIssuerId}
      connect={connect}
      fallback={fallback}
    >
      {children}
    </GatedConnection>
  );
}

function GatedConnection({
  mcpServerId,
  userSessionIssuerId,
  connect,
  fallback,
  children,
}: {
  mcpServerId: string | undefined;
  userSessionIssuerId: string;
  connect: (() => void) | undefined;
  fallback: ReactNode;
  children: ReactNode;
}): JSX.Element {
  // No focus listener needed: the gate's queries use the default staleTime of
  // 0, so react-query's refetchOnWindowFocus re-runs them when the user comes
  // back from the connect tab and the gate flips on its own.
  const hasUserConnected = useHasUserConnected({ userSessionIssuerId });

  if (hasUserConnected === undefined) {
    return <>{fallback}</>;
  }
  if (!hasUserConnected) {
    return <RemoteMcpConnectPrompt onConnect={connect} />;
  }
  return (
    <MintedConnection
      mcpServerId={mcpServerId}
      connect={connect}
      fallback={fallback}
    >
      {children}
    </MintedConnection>
  );
}

/**
 * Mounting this component mints the user-session JWT. It must stay behind
 * the hasUserConnected gate.
 */
function MintedConnection({
  mcpServerId,
  connect,
  fallback,
  children,
}: {
  mcpServerId: string | undefined;
  connect: (() => void) | undefined;
  fallback: ReactNode;
  children: ReactNode;
}): JSX.Element {
  const { accessToken, isLoading } = useRemoteMcpUserSessionToken({
    mcpServerId,
  });

  const value = useMemo<RemoteMcpConnectionValue>(
    () => ({
      headers: accessToken
        ? { Authorization: `Bearer ${accessToken}` }
        : undefined,
      connect,
    }),
    [accessToken, connect],
  );

  // Hold children until the JWT lands; an unauthenticated probe would 401
  // and cache a spurious needs-auth state.
  if (isLoading) {
    return <>{fallback}</>;
  }

  return (
    <RemoteMcpConnectionContext.Provider value={value}>
      {children}
    </RemoteMcpConnectionContext.Provider>
  );
}

/** The disconnected state: prompts the user to connect upstream. */
export function RemoteMcpConnectPrompt({
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
