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
import { MCP_AUTHENTICATION_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection";
import { ChevronRight, ExternalLink } from "lucide-react";
import { useEffect, useRef, useState, type JSX } from "react";
import { Link } from "react-router";
import {
  connectionSelectable,
  type ServerConnection,
} from "./ownerConnections";
import type { ServerSelection } from "./StepServers";
import type { OwnerConnections } from "./useOwnerConnections";
import type { InventoryServer } from "./useServerInventory";

/**
 * The servers that authorize each person separately, listed apart from the
 * ones a key reaches on its own.
 *
 * With `connections`, a row can be chosen once its owner-to-be holds a
 * working connection to it: the agent is set up with that connection now,
 * so nobody signs in when it calls. Without them (editing an agent's reach)
 * the rows are only listed, as the agent's own page connects accounts.
 */
export function PersonServers({
  servers,
  connections,
  allServers,
  selected,
  onChange,
}: {
  servers: InventoryServer[];
  connections?: OwnerConnections;
  /**
   * "All servers" names servers one by one in the key it issues, and these
   * are not among them, so they are chosen individually only while it is off.
   */
  allServers: boolean;
  selected: ServerSelection[];
  onChange: (next: ServerSelection[]) => void;
}): JSX.Element | null {
  const routes = useRoutes();
  const [open, setOpen] = useState(connections !== undefined);
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
    selected.some((entry) => entry.server.id === id);
  const toggle = (server: InventoryServer) =>
    onChange(
      chosen(server.id)
        ? selected.filter((entry) => entry.server.id !== server.id)
        : [...selected, { server }],
    );
  return (
    <div className="border-border border-t">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="hover:bg-muted/40 flex w-full items-center gap-2 px-4 py-3 text-left"
      >
        <ChevronRight
          aria-hidden="true"
          className={cn(
            "size-4 shrink-0 transition-transform",
            open && "rotate-90",
          )}
        />
        <span className="flex-1 text-sm">Servers that sign in as a person</span>
        <Badge variant="neutral" size="sm">
          {servers.length}
        </Badge>
      </button>
      {open && (
        <div className="border-border border-t">
          {/* Said once for the group: whose connection is used, why only
              theirs, and what happens when it ends. */}
          <Text muted small className="block px-4 pt-3">
            {connections ? (
              <>
                Each of these signs in as a person. Choose one and the agent
                uses your own connection to it, set up now, so nobody signs in
                when it calls. An agent uses only its owner&apos;s connections,
                never anyone else&apos;s. When a connection ends, the
                agent&apos;s calls to that server fail until you sign in again
                and choose the new connection for the agent on the server&apos;s
                Sessions tab.
                {allServers &&
                  " All servers covers none of these: turn it off to choose them one by one."}
              </>
            ) : (
              "These authorize each person separately, so an agent cannot use them until an account is connected from the agent's credentials section. A server that should answer to one shared credential instead can be changed in its settings."
            )}
          </Text>
          <ul className="divide-border divide-y">
            {servers.map((server) => (
              <li key={server.id} className="flex items-center gap-4 px-4 py-3">
                {connections && (
                  <Checkbox
                    aria-label={server.name}
                    checked={!allServers && chosen(server.id)}
                    disabled={
                      allServers ||
                      (!chosen(server.id) &&
                        !connectionSelectable(
                          connections.byServer.get(server.id),
                        ))
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
                  {connections && (
                    <ConnectionLine
                      server={server}
                      connection={connections.byServer.get(server.id)}
                      onRetry={connections.refetch}
                    />
                  )}
                </span>
                {/* A new tab, because this is a detour: the wizard holds the
                    name, scope and selections in memory, and navigating away
                    then coming back loses all of it. */}
                <Button size="sm" variant="secondary" asChild>
                  <Link
                    to={`${routes.mcp.details.settings.href(
                      mcpServerRouteParam(server),
                    )}#${MCP_AUTHENTICATION_SECTION_ID}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    Change identity
                  </Link>
                </Button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
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
