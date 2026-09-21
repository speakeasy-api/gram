import { AgentIcon, AgentLink } from "@/components/agent-link";
import { IdentityLink } from "@/components/identity-link";
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
    return (
      <span className="inline-flex min-w-0 items-center gap-2">
        <AgentIcon />
        <AgentLink agentId={agent?.id} className="truncate">
          {agent?.name ?? `agent:${identityKey}`}
        </AgentLink>
      </span>
    );
  }
  return (
    <IdentityLink identifier={identityRefForKind(kind, identityKey)}>
      {label}
    </IdentityLink>
  );
}
