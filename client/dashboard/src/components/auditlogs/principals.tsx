import type { ReactNode } from "react";
import { AgentIcon, AgentLink } from "@/components/agent-link";
import { IdentityLink } from "@/components/identity-link";
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
    const agent = identities.agents.get(principal.id);
    return (
      <span
        className="inline-flex items-center gap-1 align-middle"
        title={principal.urn}
      >
        <AgentIcon />
        <AgentLink agentId={agent?.id} className={className}>
          {renderLabel(label)}
        </AgentLink>
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
