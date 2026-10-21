import type { ReactNode } from "react";
import { IdentityLink } from "@/components/identity-link";
import { ActingPrincipal } from "@/components/ui/ActingPrincipal";
import {
  auditPrincipalLabel,
  type AuditPrincipal,
  type useAuditPrincipals,
} from "./audit-principals";

export function AuditPrincipalLink({
  principal,
  identities,
  fallback,
  className,
  projectSlug,
  renderLabel = (label) => label,
}: {
  principal: AuditPrincipal | null;
  identities: ReturnType<typeof useAuditPrincipals>;
  fallback: string;
  className?: string;
  projectSlug?: string;
  renderLabel?: (label: string) => ReactNode;
}): JSX.Element {
  const label = auditPrincipalLabel(principal, identities, fallback);
  if (!principal)
    return <span className={className}>{renderLabel(label)}</span>;
  if (principal.kind === "agent") {
    // The shared renderer draws the agent's face and links it. The audit log
    // records no authorizer, so this is the agent alone — deliberately, not
    // for want of looking: substituting the agent's current owner would
    // name someone who may not have been there at the time.
    //
    // Linked only once the agent resolved from the inventory the caller is
    // allowed to read. An id off an audit row is not permission to open that
    // agent, and linking one we could not read would offer a door that the
    // next page refuses.
    const readable = identities.agents.get(principal.id);
    return (
      <span className="align-middle" title={principal.urn}>
        <ActingPrincipal
          agent={{ id: principal.id, name: label, display: renderLabel(label) }}
          linked={Boolean(readable)}
          className={className}
        />
      </span>
    );
  }
  return (
    <IdentityLink
      identifier={{ userId: principal.id }}
      className={className}
      projectSlug={projectSlug}
    >
      {renderLabel(label)}
    </IdentityLink>
  );
}
