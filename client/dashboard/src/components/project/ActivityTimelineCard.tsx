import { IdentityLink } from "@/components/identity-link";
import { ChevronRight } from "lucide-react";
import { Link } from "react-router";
import { Link as TextLink } from "@/components/ui/Link";
import { Card } from "@/components/ui/Card";
import { Skeleton } from "@/components/ui/Skeleton";
import { ActionIconTile } from "@/components/auditlogs/feed";
import { subjectHref } from "@/components/auditlogs/subject-href";
import { useSlugs } from "@/contexts/Sdk";
import { formatSubjectLabel, renderVerb } from "@/lib/audit-log-format";
import { format, formatDistanceToNow, isToday, isYesterday } from "date-fns";
import type { AuditLog } from "@gram/client/models/components/auditlog.js";

type Props = {
  logs: AuditLog[];
  isPending: boolean;
  viewAllHref: string;
};

export function ActivityTimelineCard({
  logs,
  isPending,
  viewAllHref,
}: Props): JSX.Element {
  const logGroups = groupLogsByDate(logs);
  const { orgSlug } = useSlugs();

  return (
    <Card.Dashboard
      title="Activity Timeline"
      tooltip="Recent administrative activity in this project — changes to sources, MCP server changes, API key rotations, environment edits, and access role updates. Grouped by day, most recent first."
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
      ) : logs.length === 0 ? (
        <p className="text-muted-foreground text-sm">No recent activity</p>
      ) : (
        <div className="space-y-4">
          {logGroups.map((group) => (
            <div key={group.label}>
              <p className="text-eyebrow mb-2">{group.label}</p>
              <ul className="divide-border divide-y">
                {group.logs.map((log) => {
                  const actor =
                    log.actorDisplayName ?? log.actorSlug ?? "Unknown";
                  const actionLabel = renderVerb(log);
                  const subjectLabel = log.subjectDisplayName
                    ? formatSubjectLabel(
                        log.subjectDisplayName,
                        log.subjectType,
                      )
                    : null;
                  const href = orgSlug ? subjectHref(log, orgSlug) : null;
                  return (
                    <li
                      key={log.id}
                      className="flex items-start gap-3 py-2.5 first:pt-0 last:pb-0"
                    >
                      <ActionIconTile action={log.action} className="mt-0.5" />
                      <div className="min-w-0 flex-1">
                        <p className="text-sm">
                          <IdentityLink
                            identifier={
                              log.actorType === "user"
                                ? { userId: log.actorId }
                                : null
                            }
                            className="font-medium"
                          >
                            {actor}
                          </IdentityLink>{" "}
                          <span className="text-muted-foreground">
                            {actionLabel}
                          </span>
                          {log.subjectDisplayName && (
                            <>
                              {" "}
                              {href ? (
                                <TextLink
                                  asChild
                                  size="sm"
                                  underline={false}
                                  className="font-medium"
                                >
                                  <Link
                                    to={href}
                                    title={log.subjectDisplayName}
                                  >
                                    {subjectLabel}
                                  </Link>
                                </TextLink>
                              ) : (
                                <span
                                  className="font-medium"
                                  title={log.subjectDisplayName}
                                >
                                  {subjectLabel}
                                </span>
                              )}
                            </>
                          )}
                        </p>
                        <p className="text-muted-foreground mt-0.5 text-xs">
                          {formatDistanceToNow(log.createdAt, {
                            addSuffix: true,
                          })}
                        </p>
                      </div>
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </div>
      )}
    </Card.Dashboard>
  );
}

// --- Helpers ---

type LogGroup = { label: string; logs: AuditLog[] };

function groupLogsByDate(logs: AuditLog[]): LogGroup[] {
  const map = new Map<string, AuditLog[]>();
  // Newest first, matching the org audit log feed — the API is not guaranteed
  // to hand these back in display order.
  const ordered = [...logs].sort(
    (a, b) => b.createdAt.getTime() - a.createdAt.getTime(),
  );
  for (const log of ordered) {
    const label = isToday(log.createdAt)
      ? "Today"
      : isYesterday(log.createdAt)
        ? "Yesterday"
        : format(log.createdAt, "MMM d, yyyy");
    const group = map.get(label) ?? [];
    group.push(log);
    map.set(label, group);
  }
  return [...map.entries()].map(([label, logs]) => ({ label, logs }));
}
