import { IdentityLink } from "@/components/identity-link";
import { ActingPrincipal } from "@/components/ui/ActingPrincipal";
import { identityRefForKind } from "@/lib/identity-urn";

export function ToolUsageIdentity({
  kind,
  identityKey,
  label,
  agent,
}: {
  kind: string;
  identityKey: string;
  label: string;
  agent?: { id: string; name: string };
}): JSX.Element {
  if (kind === "agent_id") {
    // No authorizer: telemetry attributes a call to one principal, so naming a
    // person here would invent a delegation this row cannot know about.
    //
    // Linked only once the agent itself resolved — an id read off a telemetry
    // row is not proof the reader is allowed to open that agent.
    return (
      <ActingPrincipal
        agent={{ id: agent?.id ?? identityKey, name: agent?.name }}
        linked={Boolean(agent?.id)}
      />
    );
  }
  return (
    <IdentityLink identifier={identityRefForKind(kind, identityKey)}>
      {label}
    </IdentityLink>
  );
}
