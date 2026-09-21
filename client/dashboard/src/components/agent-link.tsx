import type { ReactNode } from "react";
import { Link } from "react-router";
import { Bot } from "lucide-react";
import { useRoutes } from "@/routes";
import { cn } from "@/lib/utils";

export function AgentIcon(): JSX.Element {
  return (
    <span
      role="img"
      aria-label="Agent"
      title="Agent"
      className="bg-muted flex size-6 shrink-0 items-center justify-center rounded-full"
    >
      <Bot aria-hidden="true" className="text-muted-foreground size-3.5" />
    </span>
  );
}

/** Pass an ID only after the management API has authorized reading the agent. */
export function AgentLink({
  agentId,
  children,
  className,
}: {
  agentId?: string;
  children: ReactNode;
  className?: string;
}): JSX.Element {
  const routes = useRoutes();
  if (!agentId) return <span className={className}>{children}</span>;
  return (
    <Link
      to={`${routes.agents.href()}?id=${encodeURIComponent(agentId)}`}
      onClick={(event) => event.stopPropagation()}
      className={cn(
        "decoration-foreground/30 hover:decoration-foreground underline decoration-dotted underline-offset-4",
        className,
      )}
    >
      {children}
    </Link>
  );
}
