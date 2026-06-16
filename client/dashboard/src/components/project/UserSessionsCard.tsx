import { ChevronRight } from "lucide-react";
import { Link } from "react-router";
import { useUserSessions } from "@gram/client/react-query";
import { DashboardCard } from "@/components/ui/dashboard-card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import {
  sessionStatus,
  subjectLabel,
  STATUS_PRESENTATION,
} from "@/lib/user-session-status";
import { formatDistanceToNow } from "date-fns";

export function UserSessionsCard({
  viewAllHref,
}: {
  viewAllHref: string;
}): JSX.Element {
  const { data, isPending } = useUserSessions({ status: "active", limit: 5 });
  const sessions = data?.result.items ?? [];

  return (
    <DashboardCard
      title="User Sessions"
      tooltip="Active sessions clients hold into this project's MCP servers, established via OAuth. Most recent first."
      action={
        <Link
          to={viewAllHref}
          className="text-muted-foreground hover:text-foreground flex items-center gap-0.5 text-xs no-underline"
        >
          View all
          <ChevronRight className="size-3" />
        </Link>
      }
    >
      {isPending ? (
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : sessions.length === 0 ? (
        <p className="text-muted-foreground text-sm">No active sessions</p>
      ) : (
        <ul className="divide-border divide-y">
          {sessions.map((s) => (
            <li key={s.id} className="flex items-center gap-3 py-2">
              <span
                className={cn(
                  "size-2 shrink-0 rounded-full",
                  STATUS_PRESENTATION[sessionStatus(s)].dotClass,
                )}
              />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">
                  {subjectLabel(s)}
                </p>
                <p className="text-muted-foreground truncate text-xs">
                  {s.clientName ? `${s.clientName} · ` : ""}
                  {s.issuerSlug}
                </p>
              </div>
              <span className="text-muted-foreground shrink-0 text-xs">
                expires{" "}
                {formatDistanceToNow(new Date(s.expiresAt), {
                  addSuffix: true,
                })}
              </span>
            </li>
          ))}
        </ul>
      )}
    </DashboardCard>
  );
}
