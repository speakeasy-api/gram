import type { JSX } from "react";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/Accordion";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { useSession } from "@/contexts/Auth";
import {
  connectionLifetime,
  connectionLifetimeLabel,
} from "@/components/sessions/connection-lifetime";
import { useOAuthPopup } from "@/components/sessions/useOAuthPopup";
import { AlertTriangle, RefreshCw, X } from "lucide-react";
import { ServerAccounts } from "../AgentKeyServers";
import { WizardStepHeader } from "./WizardChrome";
import type { OwnerConnections } from "./useOwnerConnections";
import type { InventoryServer } from "./useServerInventory";
import { cn } from "@/lib/utils";

export function StepAuthentication({
  servers,
  connections,
  agent,
  allServers,
  onRemove,
}: {
  servers: InventoryServer[];
  connections: OwnerConnections;
  agent?: ManagedAgent;
  allServers: boolean;
  onRemove: (id: string) => void;
}): JSX.Element {
  const { user } = useSession();
  const oauth = useOAuthPopup(connections.refetch);
  const displayedConnections = agent
    ? (connections.assignedByServer ?? connections.byServer)
    : connections.byServer;
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
        title="MCP authentication"
        description="Choose how the agent connects to each selected MCP server. Open a row to manage its connection, or remove a server here."
      />
      {allServers && missing.length > 0 && (
        <Text role="alert" warning small>
          No usable connection available for:{" "}
          {missing.map((server) => server.name).join(", ")}. Existing valid
          assignments will be kept. These servers need a connection before the
          agent can use them.
        </Text>
      )}
      {oauth.blocked && (
        <Text role="alert" warning small>
          Allow popups, then try connecting again.
        </Text>
      )}
      <Accordion type="multiple" className="border-border border">
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
            <AccordionItem key={server.id} value={server.id} className="px-4">
              <div className="flex items-center gap-3">
                <div className="min-w-0 flex-1">
                  <AccordionTrigger className="items-center hover:no-underline">
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">{server.name}</span>
                      <span className="text-muted-foreground block truncate text-xs font-normal">
                        /mcp/{server.slug}
                      </span>
                    </span>
                    {session && (
                      <Avatar
                        title={
                          session.subjectDisplayName ||
                          session.subjectEmail ||
                          "Connection owner"
                        }
                      >
                        <AvatarImage
                          src={mine ? user.photoUrl : undefined}
                          alt=""
                        />
                        <AvatarFallback>
                          {(
                            session.subjectDisplayName ||
                            session.subjectEmail ||
                            "?"
                          )
                            .slice(0, 2)
                            .toUpperCase()}
                        </AvatarFallback>
                      </Avatar>
                    )}
                    <Badge
                      variant={
                        expired || connection?.status === "missing"
                          ? "warning"
                          : "neutral"
                      }
                      size="sm"
                    >
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
                ) : agent ? (
                  <ServerAccounts
                    compact
                    agent={agent}
                    servers={[server]}
                    onReady={() => {}}
                  />
                ) : (
                  <div
                    className={cn(
                      "flex flex-wrap items-center gap-3 border p-3",
                      expired && "border-warning bg-warning/5",
                    )}
                  >
                    {session && (
                      <Avatar>
                        <AvatarImage
                          src={user.photoUrl}
                          alt={user.displayName || user.email}
                        />
                        <AvatarFallback>
                          {(user.displayName || user.email)
                            .slice(0, 2)
                            .toUpperCase()}
                        </AvatarFallback>
                      </Avatar>
                    )}
                    <div className="min-w-0 flex-1">
                      <Text small warning={expired}>
                        {expired ? "Your connection has expired" : label}
                      </Text>
                      {session && (
                        <Text small muted={!expired} warning={expired}>
                          {connectionLifetimeLabel(connectionLifetime(session))}
                        </Text>
                      )}
                    </div>
                    {server.connectUrl && (
                      <Button
                        size="sm"
                        variant={expired || !session ? "primary" : "secondary"}
                        onClick={() => {
                          oauth.open(server.connectUrl!);
                        }}
                      >
                        {expired
                          ? "Reauthenticate"
                          : session
                            ? "Use a different account"
                            : "Connect account"}
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
