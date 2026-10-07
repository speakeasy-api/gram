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
};

/**
 * The assistant a session ran as, linked to the agent identity it acts as or,
 * for an assistant without one, to the assistant itself.
 */
export function AssistantActorLink({
  chat,
  className,
}: {
  chat: AssistantActor;
  className?: string;
}): JSX.Element | null {
  const routes = useRoutes();
  if (!chat.assistantName) return null;

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
            className="block min-w-0 truncate pb-0.5"
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
            className="decoration-foreground/30 hover:decoration-foreground block min-w-0 truncate pb-0.5 underline underline-offset-4"
          >
            {name}
          </Link>,
        )}
      </SimpleTooltip>
    );
  }

  return wrap(<span className="truncate">{name}</span>);
}
