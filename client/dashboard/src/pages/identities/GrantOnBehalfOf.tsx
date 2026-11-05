import { IdentityAvatar } from "@/components/identity-avatar";
import { IdentityLink } from "@/components/identity-link";
import type { ServerConnection } from "@/pages/agents/credentials/ownerConnections";
import { useOwnerConnections } from "@/pages/agents/credentials/useOwnerConnections";
import { useServerInventory } from "@/pages/agents/credentials/useServerInventory";

/** The person whose own connection an agent signs in with on a server. */
export type OnBehalfOfPerson = {
  userId: string;
  name: string;
  /** The connection is assigned but no longer works. */
  expired: boolean;
};

const USER_PREFIX = "user:";

/**
 * Whom an agent acts for on a server, from the connection assigned to it
 * there: the owner of a person's own connection. A shared credential, a server
 * that needs no sign-in, and a connection not yet read act for no one.
 */
export function onBehalfOfPerson(
  connection: ServerConnection | undefined,
): OnBehalfOfPerson | undefined {
  if (connection?.status !== "ready" && connection?.status !== "ended") {
    return undefined;
  }
  const session = connection.sessions.find((candidate) =>
    candidate.subjectUrn.startsWith(USER_PREFIX),
  );
  if (!session) return undefined;
  const userId = session.subjectUrn.slice(USER_PREFIX.length);
  return {
    userId,
    name: session.subjectDisplayName || session.subjectEmail || userId,
    expired: connection.status === "ended",
  };
}

/**
 * "On behalf of" and the person, beside an `mcp:connect` grant whose server
 * the agent signs in to with that person's own connection. Renders nothing
 * for a shared credential, or while the connection is still being read.
 */
export function GrantOnBehalfOf({
  agentId,
  resourceId,
}: {
  agentId: string;
  /** The server id the grant's selector names. */
  resourceId: string;
}): JSX.Element | null {
  const inventory = useServerInventory();
  const server = inventory.servers.find(
    (candidate) =>
      candidate.resourceId === resourceId || candidate.id === resourceId,
  );
  const connections = useOwnerConnections(
    server ? [server] : [],
    !!server,
    agentId,
  );
  const person = onBehalfOfPerson(
    server ? connections.assignedByServer?.get(server.id) : undefined,
  );
  if (!person) return null;
  return (
    <span className="text-muted-foreground inline-flex min-w-0 items-center gap-1.5 text-xs">
      On behalf of
      <IdentityAvatar
        label={person.name}
        className="size-4"
        textClassName="text-[8px]"
      />
      <IdentityLink identifier={{ userId: person.userId }}>
        {person.name}
      </IdentityLink>
      {person.expired && <span>(connection expired)</span>}
    </span>
  );
}
