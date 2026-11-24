import { ServerInfoPopover } from "./ServerInfoPopover";
import { useEffect, useRef, useState, type JSX, type ReactNode } from "react";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/Accordion";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { IdentityAvatar } from "@/components/identity-avatar";
import { useSession } from "@/contexts/Auth";
import {
  connectionLifetime,
  connectionLifetimeLabel,
} from "@/components/sessions/connection-lifetime";
import { useOAuthPopup } from "@/components/sessions/useOAuthPopup";
import { AlertTriangle, Check, LockKeyhole, RefreshCw, X } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import {
  assignableOwnConnection,
  displayedConnections,
  missingSignInsSentence,
  refreshOutcome,
  serversNeedingConnection,
} from "./connectionAttention";
import { KeyCredentialChoice } from "./KeyCredentialChoice";
import { ACCOUNT_CHOICE } from "./keyCredentialChoices";
import { SignInPath } from "./SignInPath";
import { WizardStepHeader } from "./WizardChrome";
import type { OwnerConnections } from "./useOwnerConnections";
import type { InventoryServer } from "./useServerInventory";

export function StepAuthentication({
  servers,
  connections,
  allServers,
  onRemove,
  onConnected,
  onAssign,
  assigning,
  listAction,
  credentialChoice,
  showHeader = true,
}: {
  servers: InventoryServer[];
  connections: OwnerConnections;
  allServers: boolean;
  onRemove: (id: string) => void;
  /**
   * Called with the server once the reader's own connection to it, just
   * signed in to, is ready, for a page that assigns it as it is made.
   */
  onConnected?: (serverId: string) => void;
  /**
   * Gives an existing agent the reader's own working connection to a server
   * it has none for. Only offered where this is set: nothing assigns the
   * reader's connection to an agent unless they ask.
   */
  onAssign?: (serverId: string) => void;
  /** The servers whose assignment is being saved. */
  assigning?: ReadonlySet<string>;
  /** Shown at the top right of the server list. */
  listAction?: ReactNode;
  /**
   * How the agent's keys sign in to a server that offers shared credentials,
   * asked on the server's own row: the sign-in is this step's question, not
   * the picker's. `show` says which rows ask.
   */
  credentialChoice?: {
    show: (serverId: string) => boolean;
    value: (serverId: string) => string | undefined;
    onChange: (serverId: string, authClientId: string) => void;
  };
  /** False where the page around it already titles the step. */
  showHeader?: boolean;
}): JSX.Element {
  const { user } = useSession();
  // The server whose sign-in popup is open, and then the one whose connection
  // is being read back after it closed.
  const connecting = useRef<string | null>(null);
  const [reading, setReading] = useState<string | null>(null);
  const oauth = useOAuthPopup(async (reason) => {
    // Taken before the refetch: another sign-in opened while it runs must
    // not replace the server this one was for.
    const server = reason === "popup-closed" ? connecting.current : null;
    if (server !== null) connecting.current = null;
    await connections.refetch();
    if (server !== null) setReading(server);
  });
  // Only a connection that is now usable is worth saving: closing the popup
  // without signing in changes nothing.
  const readStatus =
    reading === null ? undefined : connections.byServer.get(reading)?.status;
  useEffect(() => {
    if (reading === null || readStatus === "checking") return;
    setReading(null);
    if (readStatus === "ready") onConnected?.(reading);
  }, [reading, readStatus, onConnected]);
  const [openRows, setOpenRows] = useState<string[]>([]);
  // Editing shows the agent's assigned connection, unless it no longer works
  // and the editor has a usable one: saving assigns that in its place,
  // exactly as creating does.
  const displayed = displayedConnections(connections);
  // The server whose connection status is being refreshed, and then the one
  // whose refreshed status is told once it is read.
  const [refreshing, setRefreshing] = useState<string | null>(null);
  const [refreshed, setRefreshed] = useState<string | null>(null);
  const refreshedConnection =
    refreshed === null ? undefined : displayed.get(refreshed);
  const refreshedName = servers.find((server) => server.id === refreshed)?.name;
  useEffect(() => {
    if (refreshed === null) return;
    if (!refreshedConnection || refreshedConnection.status === "checking")
      return;
    setRefreshed(null);
    const { tone, message } = refreshOutcome(
      refreshedName ?? "this server",
      refreshedConnection,
    );
    toast[tone](message);
  }, [refreshed, refreshedConnection, refreshedName]);
  const refresh = async (serverId: string) => {
    setRefreshing(serverId);
    try {
      await connections.refetch();
    } finally {
      setRefreshing(null);
    }
    setRefreshed(serverId);
  };
  const missing = serversNeedingConnection(servers, displayed);
  return (
    <div className="space-y-5">
      {showHeader && (
        <WizardStepHeader
          title="MCP sign-in"
          description="Choose how Speakeasy signs in to each selected MCP server when the agent calls it. Open a row to manage its connection, or remove a server here."
        />
      )}
      <SignInPath hop="mcp" onTab={!showHeader} />
      {allServers && missing.length > 0 && (
        <Text role="alert" warning small>
          No usable connection available for:{" "}
          {missing.map((server) => server.name).join(", ")}.
        </Text>
      )}
      {oauth.blocked && (
        <Text role="alert" warning small>
          Allow popups, then try connecting again.
        </Text>
      )}
      {listAction && (
        <div className="flex items-center justify-between gap-3">
          <Text small muted>
            {servers.length === 1 ? "1 server" : `${servers.length} servers`}
          </Text>
          {listAction}
        </div>
      )}
      {/* Every row starts closed: its badge already says how Speakeasy signs
          in, and a row opens only to manage that connection. */}
      <Accordion
        type="multiple"
        value={openRows}
        onValueChange={setOpenRows}
        className="border-border border"
      >
        {servers.map((server) => {
          const connection = displayed.get(server.id);
          const shared =
            !server.issuerId || connection?.status === "not-required";
          // The agent's keys use a shared credential chosen on this row, so
          // nobody's own connection is needed for them.
          const choice = credentialChoice?.show(server.id)
            ? credentialChoice.value(server.id)
            : undefined;
          const sharedChosen = !shared && !!choice && choice !== ACCOUNT_CHOICE;
          const expired = connection?.status === "ended";
          const sessions =
            connection?.status === "ready" || connection?.status === "ended"
              ? connection.sessions
              : [];
          const session = sessions[0];
          const mine = session?.subjectUrn === `user:${user.id}`;
          const owner =
            session?.subjectDisplayName ||
            session?.subjectEmail ||
            "Another member";
          // The agent has no working connection here, and the reader has one
          // they could give it.
          const assignable =
            !!onAssign &&
            !shared &&
            !sharedChosen &&
            assignableOwnConnection(connections, server.id);
          const label =
            shared || sharedChosen
              ? "Shared credential"
              : expired
                ? "Expired"
                : connection?.status === "ready"
                  ? mine
                    ? "Your connection"
                    : `${owner}'s connection`
                  : connection?.status === "checking" || !connection
                    ? "Checking connection…"
                    : connection.status === "error"
                      ? "Could not check connection"
                      : connection.status === "forbidden"
                        ? "Connection not visible to you"
                        : "Requires connection";
          const missingSignIns = sharedChosen
            ? null
            : missingSignInsSentence(server, connection);
          return (
            <AccordionItem
              key={server.id}
              value={server.id}
              className="px-4 data-[state=open]:bg-muted/30"
            >
              {/* Someone else's connection is marked by the lock on its
                  badge, not by tinting the row. */}
              <div className="flex items-center gap-3">
                <div className="min-w-0 flex-1">
                  <AccordionTrigger className="items-center hover:no-underline">
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium">
                        {server.name}
                      </span>
                    </span>
                    {session && !sharedChosen && (
                      <IdentityAvatar
                        label={
                          session.subjectDisplayName ||
                          session.subjectEmail ||
                          "Connection owner"
                        }
                        src={mine ? user.photoUrl : undefined}
                        className="size-7"
                        textClassName="text-xs"
                      />
                    )}
                    <Badge
                      // Blue, like the step's MCP sign-in arrow: each pill
                      // says how Speakeasy signs in to that server.
                      variant={
                        sharedChosen
                          ? "information"
                          : expired || connection?.status === "missing"
                            ? "warning"
                            : shared || connection?.status === "ready"
                              ? "information"
                              : "neutral"
                      }
                      size="sm"
                    >
                      {session && !mine && !sharedChosen && (
                        <LockKeyhole
                          className="mr-1 size-3"
                          aria-hidden="true"
                        />
                      )}
                      {session && mine && !expired && !sharedChosen && (
                        <Check className="mr-1 size-3" aria-hidden="true" />
                      )}
                      {expired && !sharedChosen && (
                        <AlertTriangle
                          className="mr-1 size-3"
                          aria-hidden="true"
                        />
                      )}
                      {label}
                    </Badge>
                  </AccordionTrigger>
                </div>
                <ServerInfoPopover server={server} />
                <Button
                  variant="tertiary"
                  size="sm"
                  aria-label={`Remove ${server.name}`}
                  title="Remove MCP server"
                  onClick={() => onRemove(server.id)}
                >
                  <X className="size-4" />
                </Button>
              </div>
              <AccordionContent>
                {credentialChoice?.show(server.id) &&
                  (server.kind === "Remote" || server.kind === "Tunneled") &&
                  // Whether it signs in accounts is only known once its
                  // connection has been read.
                  (!server.issuerId ||
                    (connection !== undefined &&
                      connection.status !== "checking")) && (
                    <KeyCredentialChoice
                      className="pb-3"
                      serverId={server.id}
                      serverName={server.name}
                      // A server that signs each caller in also offers the
                      // account connected for the agent, beside its shared
                      // credentials.
                      // Only once its own auth says it signs each person in:
                      // an issuer alone does not, and offering the account
                      // there would default a key past its only credential.
                      accounts={
                        !!server.issuerId &&
                        !shared &&
                        (connection?.status === "missing" ||
                          connection?.status === "ready" ||
                          connection?.status === "ended" ||
                          // A read that failed and is being retried, or one
                          // the viewer may not make, keeps a personal
                          // connection already picked, rather than clearing
                          // it as no longer offered.
                          ((connection?.status === "error" ||
                            connection?.status === "forbidden") &&
                            choice === ACCOUNT_CHOICE))
                      }
                      pickSole
                      label={`Choose how the agent's keys sign in to ${server.name}`}
                      value={credentialChoice.value(server.id)}
                      onChange={(authClientId) =>
                        credentialChoice.onChange(server.id, authClientId)
                      }
                    />
                  )}
                {connection?.status === "forbidden" && !sharedChosen ? (
                  <Text small muted className="py-1">
                    You don't have permission to see which connection this agent
                    uses for {server.name}. Ask someone who manages the agent to
                    check it.
                  </Text>
                ) : shared || sharedChosen ? (
                  <Text small muted>
                    {sharedChosen
                      ? "The agent's keys use the shared credential chosen above. No personal connection is needed."
                      : "This server uses a shared credential. No personal connection is needed."}
                  </Text>
                ) : (
                  <div className="flex flex-wrap items-center gap-3 py-1">
                    <div className="min-w-0 flex-1">
                      <Text small warning={expired} className="text-sm">
                        {expired
                          ? mine
                            ? "Your connection has expired"
                            : `${owner}'s connection has expired`
                          : label}
                      </Text>
                      {session && (
                        <Text small muted className="mt-1 text-xs">
                          {connectionLifetimeLabel(connectionLifetime(session))}
                        </Text>
                      )}
                      {missingSignIns && (
                        <Text small muted className="mt-1 text-xs">
                          {missingSignIns}
                        </Text>
                      )}
                      {assignable && (
                        <Text small muted className="mt-1 text-xs">
                          You have a working connection to {server.name}. The
                          agent can use it only once you assign it.
                        </Text>
                      )}
                    </div>
                    {assignable && (
                      <Button
                        size="xs"
                        variant="primary"
                        disabled={assigning?.has(server.id)}
                        onClick={() => onAssign?.(server.id)}
                      >
                        {assigning?.has(server.id)
                          ? "Assigning…"
                          : "Use your connection"}
                      </Button>
                    )}
                    {server.connectUrl && !assignable && (
                      <Button
                        size="xs"
                        variant={expired || !session ? "primary" : "secondary"}
                        onClick={() => {
                          const url = new URL(server.connectUrl!);
                          if (session?.remoteSessionClientId) {
                            url.searchParams.set(
                              "reauthenticate_client_id",
                              session.remoteSessionClientId,
                            );
                          }
                          if (oauth.open(url.toString()))
                            connecting.current = server.id;
                        }}
                      >
                        {session ? "Reauthenticate" : "Connect account"}
                      </Button>
                    )}
                    {!server.connectUrl && !session && !assignable && (
                      <Text small muted>
                        Connection setup is unavailable for this server.
                      </Text>
                    )}
                    <Button
                      variant="tertiary"
                      size="sm"
                      aria-label={`Refresh ${server.name} connection status`}
                      title="Refresh connection status"
                      aria-busy={refreshing === server.id}
                      disabled={refreshing === server.id}
                      onClick={() => {
                        void refresh(server.id);
                      }}
                    >
                      <RefreshCw
                        className={cn(
                          "size-4",
                          refreshing === server.id && "animate-spin",
                        )}
                      />
                    </Button>
                  </div>
                )}
              </AccordionContent>
            </AccordionItem>
          );
        })}
      </Accordion>
    </div>
  );
}
