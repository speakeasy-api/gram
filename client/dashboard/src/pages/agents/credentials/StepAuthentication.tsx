import { ServerInfoPopover } from "./ServerInfoPopover";
import { useState, type JSX } from "react";
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
import { WizardStepHeader } from "./WizardChrome";
import type { OwnerConnections } from "./useOwnerConnections";
import type { InventoryServer } from "./useServerInventory";

export function StepAuthentication({
  servers,
  connections,
  allServers,
  onRemove,
}: {
  servers: InventoryServer[];
  connections: OwnerConnections;
  allServers: boolean;
  onRemove: (id: string) => void;
}): JSX.Element {
  const { user } = useSession();
  const oauth = useOAuthPopup(connections.refetch);
  // Controlled so the first row opens even when the server list arrives after
  // mount; until someone toggles a row, the default tracks the list.
  const [openRows, setOpenRows] = useState<string[] | null>(null);
  // Editing shows the agent's assigned connection while it works; otherwise
  // the editor's own, which saving assigns, exactly as creating does.
  const displayedConnections = new Map(connections.byServer);
  for (const [serverId, assigned] of connections.assignedByServer ?? []) {
    if (assigned.status === "ready" || assigned.status === "not-required")
      displayedConnections.set(serverId, assigned);
  }
  const missing = servers.filter(
    (server) =>
      server.issuerId &&
      ["missing", "ended", "error"].includes(
        displayedConnections.get(server.id)?.status ?? "checking",
      ),
  );
  return (
    <div className="space-y-5">
      <WizardStepHeader
        title="MCP Authentication"
        description="Choose how the agent connects to each selected MCP server. Open a row to manage its connection, or remove a server here."
      />
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
      {/* The first row starts open, so the step shows what a connection
          looks like without a click. */}
      <Accordion
        type="multiple"
        value={openRows ?? (servers[0] ? [servers[0].id] : [])}
        onValueChange={setOpenRows}
        className="border-border border"
      >
        {servers.map((server) => {
          const connection = displayedConnections.get(server.id);
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
              <div
                className={
                  session && !mine
                    ? "flex items-center gap-3 bg-muted/40"
                    : "flex items-center gap-3"
                }
              >
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
                      variant={
                        expired || connection?.status === "missing"
                          ? "warning"
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
                          oauth.open(url.toString());
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
