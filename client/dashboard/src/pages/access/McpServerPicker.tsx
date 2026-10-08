import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { SearchBar } from "@/components/ui/SearchBar";
import { cn } from "@/lib/utils";
import { ChevronRight } from "lucide-react";
import { useState, type JSX } from "react";

import {
  fuzzyMatch,
  serverHandle,
  type McpAdminScope,
  type McpConnectAccess,
  type ServerWithProject,
  type ToolLimitKind,
} from "./mcpAccessModel";
import { McpServerRow } from "./McpServerRow";
import type { ServerGroup } from "./serverMerge";

export interface ServerPickerActions {
  onToggleServer: (entry: ServerWithProject, checked: boolean) => void;
  onSetServers: (ids: string[], checked: boolean) => void;
  onPickLimit: (entry: ServerWithProject, kind: ToolLimitKind) => void;
  onOpenSheet: (entry: ServerWithProject) => void;
  onForbid: (id: string) => void;
  onShowPlatformAccess: () => void;
}

/**
 * Specific servers: every server the role could reach, grouped by project.
 * The default project starts open; the rest start collapsed and say how many
 * of their servers are chosen.
 */
export function McpServerPicker({
  groups,
  access,
  coverage,
  query,
  onQueryChange,
  defaultProjectId,
  actions,
}: {
  /** The inventory, without the servers this role forbids. */
  groups: ServerGroup[];
  access: McpConnectAccess;
  coverage: Map<string, McpAdminScope>;
  query: string;
  onQueryChange: (query: string) => void;
  defaultProjectId: string | undefined;
  actions: ServerPickerActions;
}): JSX.Element {
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(
    () => new Set(defaultProjectId ? [defaultProjectId] : []),
  );
  const setGroupOpen = (projectId: string, open: boolean) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (open) next.add(projectId);
      else next.delete(projectId);
      return next;
    });

  return (
    <div className="flex flex-col gap-3">
      <SearchBar
        value={query}
        onChange={onQueryChange}
        placeholder="Search servers"
        className="bg-card w-full sm:w-80"
      />
      <div className="flex flex-col gap-2">
        {groups.map((group) => (
          <ProjectServerGroup
            key={group.projectId}
            group={group}
            access={access}
            coverage={coverage}
            query={query}
            open={expanded.has(group.projectId)}
            onOpenChange={(open) => setGroupOpen(group.projectId, open)}
            actions={actions}
          />
        ))}
      </div>
    </div>
  );
}

function ProjectServerGroup({
  group,
  access,
  coverage,
  query,
  open,
  onOpenChange,
  actions,
}: {
  group: ServerGroup;
  access: McpConnectAccess;
  coverage: Map<string, McpAdminScope>;
  query: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  actions: ServerPickerActions;
}): JSX.Element {
  const total = group.servers.length;
  const isSelected = (id: string) => coverage.has(id) || !!access.servers[id];
  const selectedCount = group.servers.filter((s) => isSelected(s.id)).length;
  const editable = group.servers.filter((s) => !coverage.has(s.id));
  const allOn = editable.length > 0 && editable.every((s) => isSelected(s.id));
  const matches = group.servers.filter((s) =>
    fuzzyMatch(query, `${s.name} ${serverHandle(s)}`),
  );
  const countLabel = `${query ? `${matches.length} of ` : ""}${total} ${
    total === 1 ? "server" : "servers"
  }`;

  return (
    <Collapsible open={open} onOpenChange={onOpenChange}>
      <div className="flex min-h-10 items-center gap-3">
        <h3 className="min-w-0">
          <CollapsibleTrigger asChild>
            <button
              type="button"
              className="hover:text-foreground flex items-center gap-1.5 text-sm font-medium"
            >
              <ChevronRight
                className={cn(
                  "h-4 w-4 shrink-0 transition-transform",
                  open && "rotate-90",
                )}
              />
              <span className="truncate">{group.projectName}</span>
            </button>
          </CollapsibleTrigger>
        </h3>
        {!open && (
          // Filled in ink once the collapsed project holds a choice, so a
          // glance down the list finds them.
          <span
            title={`${selectedCount} of ${total} servers selected`}
            aria-label={`${selectedCount} of ${total} servers selected`}
            className={cn(
              "inline-flex h-5 min-w-5 items-center justify-center px-1.5 font-mono text-xs",
              selectedCount > 0
                ? "bg-primary text-primary-foreground"
                : "bg-muted text-muted-foreground",
            )}
          >
            {selectedCount}
          </span>
        )}
        <span className="text-muted-foreground text-xs">{countLabel}</span>
        {open && (
          <Button
            variant="tertiary"
            size="xs"
            className="ml-auto"
            disabled={editable.length === 0}
            aria-label={`${allOn ? "Clear all servers in" : "Select all servers in"} ${group.projectName}`}
            onClick={() =>
              actions.onSetServers(
                editable.map((s) => s.id),
                !allOn,
              )
            }
          >
            <Button.Text>{allOn ? "Clear all" : "Select all"}</Button.Text>
          </Button>
        )}
      </div>
      <CollapsibleContent className="flex flex-col gap-1 pt-1">
        {matches.length === 0 && (
          <p className="text-muted-foreground px-1 py-2 text-sm">
            No servers match &ldquo;{query}&rdquo; in this project.
          </p>
        )}
        {matches.map((server) => {
          const entry: ServerWithProject = {
            server,
            projectId: group.projectId,
            projectName: group.projectName,
          };
          return (
            <McpServerRow
              key={server.id}
              entry={entry}
              limit={access.servers[server.id]}
              lockedBy={coverage.get(server.id)}
              onToggle={(checked) => actions.onToggleServer(entry, checked)}
              onPickLimit={(kind) => actions.onPickLimit(entry, kind)}
              onOpenSheet={() => actions.onOpenSheet(entry)}
              onForbid={() => actions.onForbid(server.id)}
              onShowPlatformAccess={actions.onShowPlatformAccess}
            />
          );
        })}
      </CollapsibleContent>
    </Collapsible>
  );
}
