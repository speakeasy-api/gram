import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { ServerAccounts } from "../AgentKeyServers";
import { mcpServerRouteParam } from "@/lib/sources";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Text } from "@/components/ui/Text";
import { sessionAccountLabel } from "@/components/sessions/session-account-identity";
import {
  connectionLifetime,
  connectionLifetimeLabel,
  lifetimeEndingSoon,
  soonestLifetime,
} from "@/components/sessions/connection-lifetime";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { useSession } from "@/contexts/Auth";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { ExternalLink } from "lucide-react";
import { useEffect, useRef, type JSX } from "react";
import { Link } from "react-router";
import {
  connectionSelectable,
  type ServerConnection,
} from "./ownerConnections";
import type { ServerSelection } from "./StepServers";
import type { OwnerConnections } from "./useOwnerConnections";
import type { InventoryServer } from "./useServerInventory";

/** Personal-connection rows in the shared server list, with assignment controls for the owner. */
export function PersonServers({
  servers,
  connections,
  agent,
  refusedServers,
  allServers,
  selected,
  onChange,
}: {
  servers: InventoryServer[];
  connections?: OwnerConnections;
  agent?: ManagedAgent;
  /** Policy grants the caller or owner cannot delegate. */
  refusedServers?: ReadonlyMap<string, string>;
  /**
   * "All servers" names servers one by one in the key it issues, and these
   * are not among them, so they are chosen individually only while it is off.
   */
  allServers: boolean;
  selected: ServerSelection[];
  onChange: (next: ServerSelection[]) => void;
}): JSX.Element | null {
  const { user } = useSession();
  // Signing in happens in another tab, so coming back is the moment the
  // connection may have appeared. The latest refetch is read through a ref so
  // the listener is registered once rather than on every render.
  const refetch = useRef(connections?.refetch);
  refetch.current = connections?.refetch;
  const watching = connections !== undefined;
  useEffect(() => {
    if (!watching) return;
    const refresh = () => {
      if (document.visibilityState === "visible") refetch.current?.();
    };
    document.addEventListener("visibilitychange", refresh);
    return () => document.removeEventListener("visibilitychange", refresh);
  }, [watching]);
  if (servers.length === 0) return null;
  const chosen = (id: string) =>
    allServers || selected.some((entry) => entry.server.id === id);
  const toggle = (server: InventoryServer) =>
    onChange(
      chosen(server.id)
        ? selected.filter((entry) => entry.server.id !== server.id)
        : [...selected, { server }],
    );
  return (
    <>
      {servers.map((server) => (
        <li
          key={server.id}
          className="flex flex-wrap items-center gap-4 px-4 py-3"
        >
          {(connections || agent) && (
            <Checkbox
              aria-label={server.name}
              checked={chosen(server.id)}
              disabled={
                allServers ||
                (!chosen(server.id) &&
                  (refusedServers?.has(server.id) ||
                    (!agent &&
                      !connectionSelectable(
                        connections?.byServer.get(server.id),
                      ))))
              }
              onCheckedChange={() => toggle(server)}
            />
          )}
          <span className="min-w-0 flex-1">
            <span className="flex flex-wrap items-center gap-2">
              <span className="truncate">{server.name}</span>
              <Badge variant="neutral" size="sm">
                {server.projectName || server.projectSlug}
              </Badge>
            </span>
            <span className="text-muted-foreground block truncate font-mono text-xs">
              /mcp/{server.slug}
            </span>
            {refusedServers?.has(server.id) && (
              <span className="mt-1 block text-xs">
                {refusedServers.get(server.id)}
              </span>
            )}
            {connections && !agent && (
              <ConnectionLine
                server={server}
                connection={connections.byServer.get(server.id)}
                onRetry={connections.refetch}
              />
            )}
          </span>
          {!agent &&
            connections?.byServer.get(server.id)?.status === "ready" && (
              <Avatar title={`Signed in as ${user.displayName || user.email}`}>
                <AvatarImage
                  src={user.photoUrl}
                  alt={user.displayName || user.email}
                />
                <AvatarFallback>
                  {(user.displayName || user.email).slice(0, 2).toUpperCase()}
                </AvatarFallback>
              </Avatar>
            )}{" "}
          {agent && chosen(server.id) && (
            <div className="w-full">
              <ServerAccounts
                compact
                agent={agent}
                servers={[server]}
                onReady={() => {}}
              />
            </div>
          )}
        </li>
      ))}
    </>
  );
}

/**
 * The server's Sessions tab, in a new tab: it builds the server's own connect
 * link from its endpoints and knows when first-party connect is not offered,
 * which a row here cannot tell from the inventory alone.
 */
function SignInLink({
  server,
  label,
}: {
  server: InventoryServer;
  label: string;
}): JSX.Element {
  const routes = useRoutes();
  return (
    <Link
      to={routes.mcp.details.sessions.href(mcpServerRouteParam(server))}
      target="_blank"
      rel="noreferrer"
      className="text-primary inline-flex items-center gap-1 underline underline-offset-4"
    >
      {label}
      <ExternalLink aria-hidden="true" className="size-3" />
    </Link>
  );
}

/** Whose connection a row would use, until when, and what to do if none. */
function ConnectionLine({
  server,
  connection,
  onRetry,
}: {
  server: InventoryServer;
  connection: ServerConnection | undefined;
  onRetry: () => void;
}): JSX.Element {
  const line = "mt-1 block text-xs";
  switch (connection?.status) {
    case undefined:
    case "checking":
      return (
        <Text as="span" muted small className={line}>
          Checking your connection…
        </Text>
      );
    case "error":
      return (
        <span className={cn(line, "flex flex-wrap items-center gap-2")}>
          <Text as="span" muted small role="alert">
            Could not check your connection to this server.
          </Text>
          <Button size="sm" variant="tertiary" onClick={onRetry}>
            Retry
          </Button>
        </span>
      );
    case "not-required":
      return (
        <Text as="span" muted small className={line}>
          No sign-in needed: the agent&apos;s key reaches it on its own.
        </Text>
      );
    case "missing":
      return (
        <Text as="span" muted small className={line}>
          You have no connection to it yet.{" "}
          <SignInLink server={server} label="Connect on its Sessions tab" />
        </Text>
      );
    case "ended":
      return (
        <Text as="span" small warning className={line}>
          Your connection to it has ended.{" "}
          <SignInLink
            server={server}
            label="Sign in again on its Sessions tab"
          />
        </Text>
      );
    case "ready":
      return <ReadyLine sessions={connection.sessions} />;
  }
}

function ReadyLine({
  sessions,
}: {
  sessions: Extract<ServerConnection, { status: "ready" }>["sessions"];
}): JSX.Element {
  const lifetime = soonestLifetime(
    sessions.map((session) => connectionLifetime(session)),
  );
  const soon = lifetimeEndingSoon(lifetime);
  // One server can ask for several upstream sign-ins, usually as the same
  // account, so each account is named once.
  const accounts = [...new Set(sessions.map(sessionAccountLabel))];
  const count =
    sessions.length === 1 ? "connection" : `${sessions.length} connections`;
  const whose = `Uses your ${count}: ${accounts.join(", ")}`;
  return (
    <Text
      as="span"
      small
      muted={!soon}
      warning={soon}
      className="mt-1 block text-xs"
    >
      {whose}
      {lifetime && ` · ${connectionLifetimeLabel(lifetime)}`}
    </Text>
  );
}
