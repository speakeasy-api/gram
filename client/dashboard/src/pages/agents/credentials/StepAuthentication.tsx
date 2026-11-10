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
import {
  displayedConnections,
  serversNeedingConnection,
} from "./connectionAttention";
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
  listAction,
  showHeader = true,
}: {
  servers: InventoryServer[];
  connections: OwnerConnections;
  allServers: boolean;
  onRemove: (id: string) => void;
  /**
   * Called once the reader's own connection to a server they just signed in
   * to is ready, for a page that saves each change as it is made.
   */
  onConnected?: () => void;
  /** Shown at the top right of the server list. */
  listAction?: ReactNode;
  /** False where the page around it already titles the step. */
  showHeader?: boolean;
}): JSX.Element {
  const { user } = useSession();
  // The server whose sign-in popup is open, and then the one whose connection
  // is being read back after it closed.
  const connecting = useRef<string | null>(null);
  const [reading, setReading] = useState<string | null>(null);
  const oauth = useOAuthPopup(async (reason) => {
    await connections.refetch();
    if (reason !== "popup-closed" || connecting.current === null) return;
    setReading(connecting.current);
    connecting.current = null;
  });
  // Only a connection that is now usable is worth saving: closing the popup
  // without signing in changes nothing.
  const readStatus =
    reading === null ? undefined : connections.byServer.get(reading)?.status;
  useEffect(() => {
    if (reading === null || readStatus === "checking") return;
    setReading(null);
    if (readStatus === "ready") onConnected?.();
  }, [reading, readStatus, onConnected]);
  const [openRows, setOpenRows] = useState<string[]>([]);
  // Editing shows the agent's assigned connection, unless it no longer works
  // and the editor has a usable one: saving assigns that in its place,
  // exactly as creating does.
  const displayed = displayedConnections(connections);
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
          const expired = connection?.status === "ended";
          const sessions =
            connection?.status === "ready" || connection?.status === "ended"
              ? connection.sessions
              : [];
          const session = sessions[0];
          const mine = session?.subjectUrn === `user:${user.id}`;
          const label = shared
            ? "Shared credential"
            : expired
              ? "Expired"
              : connection?.status === "ready"
                ? mine
                  ? "Your connection"
                  : `${session?.subjectDisplayName || session?.subjectEmail || "Another member"}'s connection`
                : connection?.status === "checking" || !connection
                  ? "Checking connection…"
                  : connection.status === "error"
                    ? "Could not check connection"
                    : "Requires connection";
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
                    {session && (
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
                        expired || connection?.status === "missing"
                          ? "warning"
                          : shared || connection?.status === "ready"
                            ? "information"
                            : "neutral"
                      }
                      size="sm"
                    >
                      {session && !mine && (
                        <LockKeyhole
                          className="mr-1 size-3"
                          aria-hidden="true"
                        />
                      )}
                      {session && mine && !expired && (
                        <Check className="mr-1 size-3" aria-hidden="true" />
                      )}
                      {expired && (
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
                {shared ? (
                  <Text small muted>
                    This server uses a shared credential. No personal connection
                    is needed.
                  </Text>
                ) : (
                  <div className="flex flex-wrap items-center gap-3 py-1">
                    <div className="min-w-0 flex-1">
                      <Text small warning={expired} className="text-sm">
                        {expired ? "Your connection has expired" : label}
                      </Text>
                      {session && (
                        <Text small muted className="mt-1 text-xs">
                          {connectionLifetimeLabel(connectionLifetime(session))}
                        </Text>
                      )}
                    </div>
                    {server.connectUrl && (
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
                    {!server.connectUrl && !session && (
                      <Text small muted>
                        Connection setup is unavailable for this server.
                      </Text>
                    )}
                    <Button
                      variant="tertiary"
                      size="sm"
                      aria-label={`Refresh ${server.name} connection status`}
                      title="Refresh connection status"
                      onClick={() => {
                        void connections.refetch();
                      }}
                    >
                      <RefreshCw className="size-4" />
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
