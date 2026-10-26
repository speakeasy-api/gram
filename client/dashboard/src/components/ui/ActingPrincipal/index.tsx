import { Bot, CornerDownRight } from "lucide-react";
import { useIdentityTint } from "@/components/gradient-colors";
import { IdentityLink } from "@/components/identity-link";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { cn } from "@/lib/utils";
import * as React from "react";

/**
 * Who did a thing, when an agent did it for someone.
 *
 * An agent acts under a person's authority, and naming only one of the two is
 * wrong in both directions: the agent alone hides who is accountable, and the
 * person alone hides that a machine made the call. Said once, in one
 * component, so every surface words it the same way.
 *
 * The relation gets its own line rather than running on after the agent's
 * name. Inline, the second name is the first thing a narrow column truncates,
 * which loses exactly the part that is easy to miss and hard to guess; on its
 * own line neither name competes for the width.
 *
 * The authorizer is optional because plenty of agents have none — a key or a
 * federated platform signs in as the agent and no human stands behind that
 * particular call. Those render as the agent alone, not as "on behalf of
 * unknown", which would imply a person we failed to look up.
 */
import type { Principal } from "./text";

// Underlined on hover, in full, so each name reads as its own way to its
// identity page. The offset is kept small: `truncate` clips overflow, and
// IdentityLink's default offset put the underline below the clip, so it
// never showed at all.
const LINK_CLASS =
  "min-w-0 truncate no-underline underline-offset-2 hover:underline";

export function ActingPrincipal({
  agent,
  authorizer,
  className,
  linked = false,
  projectSlug,
}: {
  agent: {
    id: string;
    name?: string | undefined;
    /**
     * What to draw in place of the name — a search-highlighted version of
     * it, say. The plain `name` is still required: it is what the tooltip
     * and the text helper use, and what a caller sorts or copies.
     */
    display?: React.ReactNode;
  };
  /**
   * Who the agent acted for. One person, or the room it acted in — a channel
   * session has no single authorizer behind it, and listing the participants
   * is the honest answer rather than picking one or showing none.
   */
  authorizer?: Principal | Principal[] | undefined;
  className?: string;
  /**
   * Whether each half links to its identity page.
   *
   * Off by default, because only the caller knows the reader may open these
   * pages: an id read off a telemetry row, a chat row or an audit entry is
   * not permission to open that agent, so a caller turns this on once the id
   * resolved through an inventory it is allowed to read. A default that
   * linked meant every new call site silently offered a door the next page
   * might refuse. It also stays off inside something that is already a link
   * or a row-level click target, where a nested link swallows the outer one.
   */
  linked?: boolean;
  /**
   * Project both identity pages open in, for a surface that picks its project
   * through a filter rather than through the address — the destination is a
   * project-level page, and a caller that knows its project says so rather
   * than letting the link fall back to whichever project its requests use.
   * See `useIdentityHrefBuilder`.
   */
  projectSlug?: string;
}): React.JSX.Element {
  // The id is a true label when the name has not resolved; an empty span is
  // not. Agents are renamable, so the id is also the stable one.
  const agentLabel = agent.name || `agent:${agent.id}`;
  const people = authorizer
    ? Array.isArray(authorizer)
      ? authorizer
      : [authorizer]
    : [];
  const first = people[0];

  return (
    <span className={cn("flex min-w-0 flex-col gap-0.5", className)}>
      <span className="flex min-w-0 items-center gap-2">
        <AgentFace />
        {linked ? (
          <IdentityLink
            identifier={{ urn: `agent:${agent.id}` }}
            projectSlug={projectSlug}
            className={LINK_CLASS}
          >
            {agent.display ?? agentLabel}
          </IdentityLink>
        ) : (
          <span className="min-w-0 truncate">
            {agent.display ?? agentLabel}
          </span>
        )}
      </span>
      {first && (
        // Indented under the agent: the relation belongs to the line above
        // it rather than standing as a second subject of its own.
        //
        // `pl-2` puts the arrow's corner on the centre line of the agent's
        // face above it, so the line reads as descending from the agent
        // rather than from the row. The glyph's own stroke sits 1/6 in from
        // its left edge, which is the 2px the padding is making up: 8px of
        // padding plus 2px of glyph lands on the 10px centre of a size-5
        // face.
        <span className="text-muted-foreground flex min-w-0 items-center gap-1.5 pl-2 text-xs">
          <CornerDownRight aria-hidden="true" className="size-3 shrink-0" />
          <span className="shrink-0">on behalf of</span>
          {/* Overlapped faces once there is more than one: a column of
              separate avatars would be as wide as the row. */}
          <span className="flex shrink-0 -space-x-1">
            {people.slice(0, 5).map((person) => (
              <PersonFace key={person.id} person={person} />
            ))}
          </span>
          {people.length === 1 ? (
            // `first.id` is not an identity reference: a chat participant is
            // keyed "slack:U123", a provider handle the resolver cannot
            // address. Only the ref the caller carried links.
            linked && first.identity ? (
              <IdentityLink
                identifier={first.identity}
                projectSlug={projectSlug}
                className={LINK_CLASS}
              >
                {first.name || first.id}
              </IdentityLink>
            ) : (
              <span className="min-w-0 truncate">{first.name || first.id}</span>
            )
          ) : (
            <span
              className="min-w-0 truncate"
              title={people
                .map((person) => person.name || person.id)
                .join(", ")}
            >
              {people.length} people
            </span>
          )}
        </span>
      )}
    </span>
  );
}

/**
 * Square, so an agent never reads as one of the round faces beside it.
 *
 * Draws the glyph rather than reusing `AgentIcon`, which is itself a round
 * 24px disc: nesting it put a circle inside this square. It carries the same
 * label, because nothing else in the rendered name says a machine acted —
 * hiding it left a screen reader with a bare name.
 */
function AgentFace(): React.JSX.Element {
  return (
    <span
      role="img"
      aria-label="Agent"
      className="bg-muted text-muted-foreground flex size-5 shrink-0 items-center justify-center"
    >
      <Bot aria-hidden="true" className="size-3" />
    </span>
  );
}

/**
 * A person's face, exported so a surface showing a human in the same column
 * as an agent draws the identical mark. A generic outline icon beside a
 * tinted initial read as two different kinds of person.
 */
export function PersonFace({
  person,
}: {
  person: { id: string; name?: string | undefined; photoUrl?: string };
}): React.JSX.Element {
  const label = person.name || person.id;
  const tint = useIdentityTint(person.id || label);
  return (
    <Avatar aria-hidden="true" className="size-4 shrink-0">
      {person.photoUrl && <AvatarImage src={person.photoUrl} alt="" />}
      <AvatarFallback className="text-[8px] font-semibold" style={tint}>
        {initials(label)}
      </AvatarFallback>
    </Avatar>
  );
}

/** Two letters, matching the member facepile so the two read as one system. */
function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0]!.slice(0, 2).toUpperCase();
  return (parts[0]![0]! + parts[parts.length - 1]![0]!).toUpperCase();
}
