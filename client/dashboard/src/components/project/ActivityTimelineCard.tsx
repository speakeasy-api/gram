import {
  ChevronRight,
  Clock,
  File,
  FileCode,
  FolderOpen,
  Globe,
  KeyRound,
  Link2,
  Package,
  Puzzle,
  Rocket,
  Shield,
  Sparkles,
  Trash2,
  type LucideIcon,
} from "lucide-react";
import { Link } from "react-router";
import { DashboardCard } from "@/components/ui/dashboard-card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { format, formatDistanceToNow, isToday, isYesterday } from "date-fns";
import type { AuditLog } from "@gram/client/models/components";

type Props = {
  logs: AuditLog[];
  isPending: boolean;
  viewAllHref: string;
};

export function ActivityTimelineCard({ logs, isPending, viewAllHref }: Props) {
  const logGroups = groupLogsByDate(logs);

  return (
    <DashboardCard
      title="Activity Timeline"
      tooltip="Recent administrative activity in this project — deployments, MCP server changes, API key rotations, environment edits, and access role updates. Grouped by day, most recent first."
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
              <p className="text-muted-foreground mb-2 text-xs font-medium tracking-wide uppercase">
                {group.label}
              </p>
              <ul className="divide-border divide-y">
                {group.logs.map((log) => {
                  const meta = getActionMeta(log.action);
                  const actor =
                    log.actorDisplayName ?? log.actorSlug ?? "Unknown";
                  const actionLabel = getActionLabel(log.action);
                  return (
                    <li
                      key={log.id}
                      className="flex items-start gap-3 py-2.5 first:pt-0 last:pb-0"
                    >
                      <div
                        className={cn(
                          "mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg",
                          meta.bg,
                        )}
                      >
                        <meta.icon className={cn("size-4", meta.fg)} />
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="text-sm">
                          <span className="font-medium">{actor}</span>{" "}
                          <span className="text-muted-foreground">
                            {actionLabel}
                          </span>
                          {log.subjectDisplayName && (
                            <>
                              {" "}
                              <span className="font-medium">
                                {log.subjectDisplayName}
                              </span>
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
    </DashboardCard>
  );
}

// --- Helpers ---

type ActionMeta = { icon: LucideIcon; bg: string; fg: string };

function getActionMeta(action: string): ActionMeta {
  if (
    action.includes(":delete") ||
    action.includes(":revoke") ||
    action.includes(":remove")
  ) {
    return {
      icon: Trash2,
      bg: "bg-red-100 dark:bg-red-950",
      fg: "text-red-600 dark:text-red-400",
    };
  }
  if (action.startsWith("deployments:")) {
    return {
      icon: Rocket,
      bg: "bg-blue-100 dark:bg-blue-950",
      fg: "text-blue-600 dark:text-blue-400",
    };
  }
  if (action.startsWith("api_key:")) {
    return {
      icon: KeyRound,
      bg: "bg-purple-100 dark:bg-purple-950",
      fg: "text-purple-600 dark:text-purple-400",
    };
  }
  if (
    action.startsWith("access_role:") ||
    action.startsWith("access_member:")
  ) {
    return {
      icon: Shield,
      bg: "bg-orange-100 dark:bg-orange-950",
      fg: "text-orange-600 dark:text-orange-400",
    };
  }
  if (action.startsWith("toolset:") && action.includes("oauth")) {
    return {
      icon: Link2,
      bg: "bg-indigo-100 dark:bg-indigo-950",
      fg: "text-indigo-600 dark:text-indigo-400",
    };
  }
  if (action.startsWith("toolset:")) {
    return {
      icon: Package,
      bg: "bg-sky-100 dark:bg-sky-950",
      fg: "text-sky-600 dark:text-sky-400",
    };
  }
  if (
    action.startsWith("environment:") ||
    action.startsWith("custom_domains:")
  ) {
    return {
      icon: Globe,
      bg: "bg-teal-100 dark:bg-teal-950",
      fg: "text-teal-600 dark:text-teal-400",
    };
  }
  if (action.startsWith("template:")) {
    return {
      icon: FileCode,
      bg: "bg-amber-100 dark:bg-amber-950",
      fg: "text-amber-600 dark:text-amber-400",
    };
  }
  if (action.startsWith("project:")) {
    return {
      icon: FolderOpen,
      bg: "bg-green-100 dark:bg-green-950",
      fg: "text-green-600 dark:text-green-400",
    };
  }
  if (action.startsWith("asset:")) {
    return {
      icon: File,
      bg: "bg-muted",
      fg: "text-muted-foreground",
    };
  }
  if (action.startsWith("variation:")) {
    return {
      icon: Sparkles,
      bg: "bg-violet-100 dark:bg-violet-950",
      fg: "text-violet-600 dark:text-violet-400",
    };
  }
  if (action.startsWith("plugin:")) {
    return {
      icon: Puzzle,
      bg: "bg-pink-100 dark:bg-pink-950",
      fg: "text-pink-600 dark:text-pink-400",
    };
  }
  return {
    icon: Clock,
    bg: "bg-muted",
    fg: "text-muted-foreground",
  };
}

const ACTION_LABELS: Record<string, string> = {
  "deployments:create": "deployed",
  "deployments:evolve": "evolved deployment",
  "deployments:redeploy": "redeployed",
  "api_key:create": "created API key",
  "api_key:revoke": "revoked API key",
  "access_role:create": "created role",
  "access_role:update": "updated role",
  "access_role:delete": "deleted role",
  "access_member:update_role": "updated member role",
  "project:create": "created project",
  "project:update": "updated project",
  "project:delete": "deleted project",
  "toolset:create": "added",
  "toolset:update": "updated MCP server",
  "toolset:delete": "deleted MCP server",
  "toolset:attach_external_oauth": "connected OAuth",
  "toolset:detach_external_oauth": "disconnected OAuth",
  "toolset:attach_oauth_proxy": "attached OAuth proxy",
  "toolset:update_oauth_proxy": "updated OAuth proxy",
  "toolset:detach_oauth_proxy": "detached OAuth proxy",
  "environment:create": "created environment",
  "environment:update": "updated environment",
  "environment:delete": "deleted environment",
  "custom_domains:create": "added custom domain",
  "custom_domains:delete": "removed custom domain",
  "template:create": "created template",
  "template:update": "updated template",
  "template:delete": "deleted template",
  "asset:create": "created asset",
  "variation:update_global": "updated variation",
  "variation:delete_global": "deleted variation",
  "plugin:create": "created plugin",
  "plugin:update": "updated plugin",
  "plugin:delete": "deleted plugin",
  "plugin:server_add": "added server to plugin",
  "plugin:server_update": "updated plugin server",
  "plugin:server_remove": "removed server from plugin",
  "plugin:assignments_set": "updated plugin access",
  "plugin:publish": "published plugins",
};

function getActionLabel(action: string): string {
  return ACTION_LABELS[action] ?? action;
}

type LogGroup = { label: string; logs: AuditLog[] };

function groupLogsByDate(logs: AuditLog[]): LogGroup[] {
  const map = new Map<string, AuditLog[]>();
  for (const log of logs) {
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
