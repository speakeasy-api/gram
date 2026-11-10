import { ActingPrincipal } from "@/components/ui/ActingPrincipal";
import { Icon } from "@/components/ui/Icon";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useReadableAgents } from "@/hooks/useReadableAgents";
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
  // A chat row carries the agent's id, which is not permission to open that
  // agent: a reader who may read the chat may not be allowed the agent. One
  // cached inventory request serves every row, and an agent missing from it
  // stays plain text rather than offering a door the next page refuses.
  //
  // A channel session has no assistant record, so the agent principal that
  // produced it is the only thing that names who made the calls.
  const agentId = chat.assistantName ? chat.assistantAgentId : chat.agentId;
  const agents = useReadableAgents(Boolean(agentId));
  const readable =
    !agents.isError &&
    (agents.data ?? []).some((agent) => agent.id === agentId);

  if (!chat.assistantName) {
    if (!chat.agentId) return null;
    // No name means the agent no longer resolves in the organization; its id
    // stays plain text rather than linking to a page reporting it missing.
    const resolved = Boolean(chat.agentName);
    return (
      <SimpleTooltip
        tooltip={
          resolved
            ? "Agent identity this session ran as"
            : "The agent this session ran as has since been deleted"
        }
      >
        <span className={cn("inline-flex min-w-0", className)}>
          <ActingPrincipal
            agent={{ id: chat.agentId, name: chat.agentName }}
            linked={resolved && readable}
            className="text-sm"
          />
        </span>
      </SimpleTooltip>
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
    // The shared renderer, so an assistant acting as an agent carries the
    // same face as the agent does everywhere else. No authorizer: a chat row
    // records the assistant and its agent, not whoever approved the session.
    return (
      <SimpleTooltip tooltip="Agent identity this assistant acts as">
        <span className={cn("inline-flex min-w-0", className)}>
          <ActingPrincipal
            agent={{ id: chat.assistantAgentId, name }}
            linked={readable}
            className="text-sm"
          />
        </span>
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
