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
 * the agent that made the calls. The name falls back to the id's short form
 * because an agent deleted since capture no longer resolves to one.
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
  return (
    <SimpleTooltip tooltip="Agent identity this session ran as">
      <span className={cn("inline-flex min-w-0 items-center gap-1", className)}>
        <Icon name="bot" className="size-3.5 shrink-0 opacity-60" />
        <IdentityLink
          identifier={{ urn: `agent:${agentId}` }}
          className="block min-w-0 truncate"
        >
          {agentName ?? agentId.slice(0, 8)}
        </IdentityLink>
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
