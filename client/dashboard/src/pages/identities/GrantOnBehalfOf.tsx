import { IdentityLink } from "@/components/identity-link";
import { PersonFace } from "@/components/ui/ActingPrincipal";
import type { OnBehalfOfPerson } from "./onBehalfOf";
import { useServerOnBehalfOf } from "./useServerOnBehalfOf";

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
  const actingFor = useServerOnBehalfOf(agentId, resourceId);
  if (actingFor.kind !== "people") return null;
  return <OnBehalfOfPeople people={actingFor.people} />;
}

/**
 * The same answer as `GrantOnBehalfOf`, said for every server: a list of the
 * agent's servers needs a line on each, so a shared credential and a missing
 * connection are named rather than left blank.
 */
export function ServerOnBehalfOfLine({
  agentId,
  resourceId,
}: {
  agentId: string;
  resourceId: string | undefined;
}): JSX.Element {
  const actingFor = useServerOnBehalfOf(agentId, resourceId);
  switch (actingFor.kind) {
    case "people":
      return <OnBehalfOfPeople people={actingFor.people} />;
    case "shared":
      return <>Shared credential, acts for no one</>;
    case "missing":
      return <>Needs a person's connection, none assigned</>;
    case "error":
      return <>Could not check whose credential it uses</>;
    case "unknown":
      return <>Credential not known</>;
    case "checking":
      return <>Checking its credential…</>;
  }
}

function OnBehalfOfPeople({
  people,
}: {
  people: OnBehalfOfPerson[];
}): JSX.Element {
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
