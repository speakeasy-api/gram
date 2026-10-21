import { AgentLink } from "@/components/agent-link";
import { IdentityLink } from "@/components/identity-link";
import { Icon } from "@/components/ui/Icon";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useRoutes } from "@/routes";
import { cn } from "@/lib/utils";
import type { JSX } from "react";
import { Link } from "react-router";

type AssistantActor = {
  assistantId?: string;
  assistantName?: string;
  assistantAgentId?: string;
  agentId?: string;
  agentName?: string;
};

/**
 * The agent principal a session ran as, when no assistant produced it. A
 * channel session has no assistant record, so this is the only thing that names
 * the agent that made the calls.
 *
 * A name means the agent still resolves from the project's own organization, so
 * only then is the identity page reachable; an agent deleted since capture
 * resolves to no name, and its short id renders as plain text rather than a link
 * to a page that would report the identity as missing. AgentLink applies the
 * same rule, hence passing the id only once it resolved.
 */
function ActingAgentLink({
  agentId,
  agentName,
  className,
}: {
  agentId: string;
  agentName?: string;
  className?: string;
}): JSX.Element {
  const resolved = Boolean(agentName);
  return (
    <SimpleTooltip
      tooltip={
        resolved
          ? "Agent identity this session ran as"
          : "The agent this session ran as has since been deleted"
      }
    >
      <span className={cn("inline-flex min-w-0 items-center gap-1", className)}>
        <Icon name="bot" className="size-3.5 shrink-0 opacity-60" />
        <AgentLink
          agentId={resolved ? agentId : undefined}
          className="block min-w-0 truncate"
        >
          {agentName ?? agentId.slice(0, 8)}
        </AgentLink>
      </span>
    </SimpleTooltip>
  );
}

/**
 * The assistant a session ran as, linked to the agent identity it acts as or,
 * for an assistant without one, to the assistant itself. A session with no
 * assistant falls back to the agent principal that produced it.
 */
export function AssistantActorLink({
  chat,
  className,
}: {
  chat: AssistantActor;
  className?: string;
}): JSX.Element | null {
  const routes = useRoutes();
  if (!chat.assistantName) {
    if (!chat.agentId) return null;
    return (
      <ActingAgentLink
        agentId={chat.agentId}
        agentName={chat.agentName}
        className={className}
      />
    );
  }

  const name = chat.assistantName;
  const wrap = (content: JSX.Element) => (
    <span className={cn("inline-flex min-w-0 items-center gap-1", className)}>
      <Icon name="bot" className="size-3.5 shrink-0 opacity-60" />
      {content}
    </span>
  );

  if (chat.assistantAgentId) {
    return (
      <SimpleTooltip tooltip="Agent identity this assistant acts as">
        {wrap(
          <IdentityLink
            identifier={{ urn: `agent:${chat.assistantAgentId}` }}
            className="block min-w-0 truncate"
          >
            {name}
          </IdentityLink>,
        )}
      </SimpleTooltip>
    );
  }

  if (chat.assistantId) {
    return (
      <SimpleTooltip tooltip="Assistant">
        {wrap(
          <Link
            to={routes.assistants.detail.href(chat.assistantId)}
            onClick={(event) => event.stopPropagation()}
            className="decoration-foreground/30 hover:decoration-foreground block min-w-0 truncate underline underline-offset-4"
          >
            {name}
          </Link>,
        )}
      </SimpleTooltip>
    );
  }

  return wrap(<span className="truncate">{name}</span>);
}
