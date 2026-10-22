import { IdentityLink } from "@/components/identity-link";
import { ActingPrincipal } from "@/components/ui/ActingPrincipal";
import { identityRefForKind } from "@/lib/identity-urn";

export function ToolUsageIdentity({
  kind,
  identityKey,
  label,
  agent,
  authorizer,
}: {
  kind: string;
  identityKey: string;
  label: string;
  agent?: { id: string; name: string };
  /**
   * The person the call was made for, when the row recorded one. Omitted for
   * aggregates, which fold many calls into one row and so have no single
   * person behind them, and for agents that sign in as themselves.
   */
  authorizer?: { userId: string; label?: string | undefined };
}): JSX.Element {
  if (kind === "agent_id") {
    // Linked only once the agent itself resolved — an id read off a telemetry
    // row is not proof the reader is allowed to open that agent.
    return (
      <ActingPrincipal
        agent={{ id: agent?.id ?? identityKey, name: agent?.name }}
        authorizer={
          authorizer
            ? {
                id: authorizer.userId,
                name: authorizer.label,
                identity: { userId: authorizer.userId },
              }
            : undefined
        }
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
