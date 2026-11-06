import { IdentityLink } from "@/components/identity-link";
import { PersonFace } from "@/components/ui/ActingPrincipal";
import { useOwnerConnections } from "@/pages/agents/credentials/useOwnerConnections";
import { useServerInventory } from "@/pages/agents/credentials/useServerInventory";
import { onBehalfOfPeople } from "./onBehalfOf";

/**
 * "On behalf of" and each person, beside an `mcp:connect` grant whose server
 * the agent signs in to with people's own connections. Renders nothing for a
 * shared credential, or while the connection is still being read.
 *
 * Draws each person with `ActingPrincipal`'s face, so they look the same here
 * as on the calls the agent made for them, but not the whole component: that
 * leads with the agent, which this page already is.
 */
export function GrantOnBehalfOf({
  agentId,
  resourceId,
}: {
  agentId: string;
  /** The server id the grant's selector names. */
  resourceId: string;
}): JSX.Element | null {
  const inventory = useServerInventory({ lookupOnly: true });
  const server = inventory.servers.find(
    (candidate) =>
      candidate.resourceId === resourceId || candidate.id === resourceId,
  );
  const connections = useOwnerConnections(
    server ? [server] : [],
    !!server,
    agentId,
  );
  const people = onBehalfOfPeople(
    server ? connections.assignedByServer?.get(server.id) : undefined,
  );
  if (people.length === 0) return null;
  return (
    <span className="text-muted-foreground inline-flex min-w-0 flex-wrap items-center gap-1.5 text-xs">
      On behalf of
      {people.map((person, index) => (
        <span
          key={person.userId}
          className="inline-flex min-w-0 items-center gap-1.5"
        >
          <PersonFace person={{ id: person.userId, name: person.name }} />
          {/* Name, marker and comma flow as one run of text: as flex
              items, the gap would put a space before the comma. */}
          <span className="min-w-0">
            <IdentityLink identifier={{ userId: person.userId }}>
              {person.name}
            </IdentityLink>
            {person.expired && <span> (connection expired)</span>}
            {index < people.length - 1 && ","}
          </span>
        </span>
      ))}
    </span>
  );
}
