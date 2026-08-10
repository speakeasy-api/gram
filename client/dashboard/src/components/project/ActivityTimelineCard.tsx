import { ChevronRight } from "lucide-react";
import { Link } from "react-router";
import { DashboardCard } from "@/components/ui/DashboardCard";
import { Skeleton } from "@/components/ui/Skeleton";
import { ActionIconTile } from "@/components/auditlogs/feed";
import { subjectHref } from "@/components/auditlogs/subject-href";
import { useSlugs } from "@/contexts/Sdk";
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
    <DashboardCard
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
                  const actionLabel = getActionLabel(log);
                  const href = orgSlug ? subjectHref(log, orgSlug) : null;
                  return (
                    <li
                      key={log.id}
                      className="flex items-start gap-3 py-2.5 first:pt-0 last:pb-0"
                    >
                      <ActionIconTile action={log.action} className="mt-0.5" />
                      <div className="min-w-0 flex-1">
                        <p className="text-sm">
                          <span className="font-medium">{actor}</span>{" "}
                          <span className="text-muted-foreground">
                            {actionLabel}
                          </span>
                          {log.subjectDisplayName && (
                            <>
                              {" "}
                              {href ? (
                                <Link
                                  to={href}
                                  className="font-medium hover:underline"
                                >
                                  {log.subjectDisplayName}
                                </Link>
                              ) : (
                                <span className="font-medium">
                                  {log.subjectDisplayName}
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
    </DashboardCard>
  );
}

// --- Helpers ---

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
  "organization_invitation:create": "sent invite to",
  "organization_invitation:revoke": "revoked invite for",
  "organization_invitation:update_role": "changed invite role for",
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
  "chat_session:access": "accessed chat session",
};

function recordString(value: unknown, key: string): string | undefined {
  if (value == null || typeof value !== "object") return undefined;
  const field = (value as Record<string, unknown>)[key];
  return typeof field === "string" && field !== "" ? field : undefined;
}

function formatRoleSlug(roleSlug: string) {
  return roleSlug.replace(/[-_]/g, " ");
}

function getInviteActionLabel(log: AuditLog): string | undefined {
  if (log.action === "organization_invitation:create") {
    const role = recordString(log.metadata, "role_slug");
    return role ? `sent ${formatRoleSlug(role)} invite to` : undefined;
  }

  if (log.action === "organization_invitation:update_role") {
    const before =
      recordString(log.beforeSnapshot, "RoleSlug") ??
      recordString(log.beforeSnapshot, "role_slug");
    const after =
      recordString(log.afterSnapshot, "RoleSlug") ??
      recordString(log.afterSnapshot, "role_slug");
    if (before && after && before !== after) {
      return `changed invite role from ${formatRoleSlug(before)} to ${formatRoleSlug(after)} for`;
    }
    if (after) {
      return `changed invite role to ${formatRoleSlug(after)} for`;
    }
  }

  return undefined;
}

function getActionLabel(log: AuditLog): string {
  return getInviteActionLabel(log) ?? ACTION_LABELS[log.action] ?? log.action;
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
