import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { SearchBar } from "@/components/ui/SearchBar";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import {
  Ban,
  Check,
  ChevronDown,
  ChevronRight,
  Info,
  RotateCcw,
  X,
} from "lucide-react";
import { useMemo, useState, type JSX, type ReactNode } from "react";

import {
  dispositionCopy,
  fuzzyMatch,
  indexServers,
  serverHandle,
  type ServerWithProject,
  type ToolLimit,
} from "./mcpAccessModel";
import { computeRuleLabel } from "./roleDialogState";
import {
  LEVEL_LABEL,
  LEVELS,
  type Level,
  levelRank,
  type McpRow,
  mcpAllows,
  mcpBlocks,
  mcpRowKey,
  mcpToolLimit,
  type OtherMcpRule,
  otherMcpRules,
  parentRow,
  type PlatformArea,
  platformAllows,
  platformAreas,
  platformBlock,
  removeOtherMcpRule,
  setMcpAllow,
  setMcpBlock,
  setMcpRowBlocked,
  setPlatformAllow,
  setPlatformBlocked,
  limitMcpUse,
  unblockPlatformScope,
  clearMcp,
  clearPlatform,
  extraCopy,
} from "./roleMatrixModel";
import { ToolAccessSheet, type ToolAccessTarget } from "./ToolAccessSheet";
import type { RoleGrant } from "./types";
import { isProjectScopedResourceType } from "./types";
import type { OrgMcpServers } from "./useOrgMcpServers";

type Grants = Record<string, RoleGrant>;
type ProjectRef = { id: string; name: string; slug?: string };

/** Where each MCP level is stored in the form's grants. */
const MCP_SCOPE_KEYS: Record<Level, string> = {
  use: "mcp:connect",
  view: "mcp:read",
  manage: "mcp:write",
};

/** Above this many servers the table gets a search box. */
const SEARCH_THRESHOLD = 8;

/**
 * What a role can do, as two grids: MCP servers by Use / View / Manage /
 * Blocked, then the rest of the platform by View / Manage / Blocked. Ticks
 * the role stores are solid; ticks it gets from a higher level or a broader
 * row are dashed, so an admin sees where access comes from without opening
 * anything.
 */
export function RoleAccessMatrix({
  grants,
  onChange,
  scopes,
  projects,
  inventory,
  loading,
  markAgentIneligible,
  onEditRule,
  onRemoveRule,
}: {
  grants: Grants;
  onChange: (change: (prev: Grants) => Grants) => void;
  scopes: ScopeDefinition[];
  projects: ProjectRef[];
  inventory: OrgMcpServers;
  /** An existing role's grants are still loading. */
  loading: boolean;
  /** Mark permissions agents cannot hold, once the role has an agent. */
  markAgentIneligible: boolean;
  /** Narrow one rule of a platform permission to particular projects. */
  onEditRule: (scope: Scope, ruleIndex: number) => void;
  /** Take back one rule of a platform permission. */
  onRemoveRule: (scope: Scope, ruleIndex: number) => void;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-4">
      <Legend />
      <Tabs defaultValue="mcp" className="border-border gap-0 border">
        <PageTabsList className="bg-muted/40 border-border w-full border-b px-4">
          <PageTabsTrigger value="mcp">MCP servers</PageTabsTrigger>
          <PageTabsTrigger value="platform">Platform access</PageTabsTrigger>
        </PageTabsList>
        <TabsContent value="mcp" className="bg-card mt-0">
          {loading ? (
            <MatrixSkeleton />
          ) : (
            <McpMatrix
              grants={grants}
              onChange={onChange}
              projects={projects}
              inventory={inventory}
            />
          )}
        </TabsContent>
        <TabsContent value="platform" className="bg-card mt-0">
          {loading ? (
            <MatrixSkeleton />
          ) : (
            <PlatformMatrix
              grants={grants}
              onChange={onChange}
              scopes={scopes}
              projects={projects}
              markAgentIneligible={markAgentIneligible}
              onEditRule={onEditRule}
              onRemoveRule={onRemoveRule}
            />
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}

// ─── Cells ──────────────────────────────────────────────────────────────────

type CellState =
  | "on"
  | "inherited"
  | "blocked"
  | "inherited-blocked"
  | "row-blocked"
  | "off";

const CELL_BASE =
  "inline-flex h-8 w-9 shrink-0 items-center justify-center border align-middle text-sm transition-colors focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none";

const CELL_STYLE: Record<CellState, string> = {
  // Emerald rather than the design system's olive success green, which reads
  // as disabled at this size. Outline only, so a full grid stays calm.
  on: "border-emerald-700 text-emerald-700 bg-card dark:border-emerald-500 dark:text-emerald-500",
  inherited: "border-foreground border-dashed text-foreground bg-card",
  blocked: "border-destructive text-destructive bg-card",
  "inherited-blocked":
    "border-destructive/40 border-dashed text-destructive/70 bg-card",
  // The Blocked column itself, once the whole row is blocked.
  "row-blocked": "bg-destructive border-destructive text-white",
  off: "border-border hover:border-foreground/40 bg-card",
};

function CellGlyph({ state }: { state: CellState }): JSX.Element | null {
  if (state === "on" || state === "inherited" || state === "row-blocked") {
    return <Check aria-hidden="true" className="size-4" />;
  }
  if (state === "blocked" || state === "inherited-blocked") {
    return <Ban aria-hidden="true" className="size-4" />;
  }
  return null;
}

/**
 * One level on one row. Only what the role stores can be clicked; a tick that
 * comes from somewhere else says where in a tooltip.
 */
function Cell({
  state,
  label,
  reason,
  onToggle,
}: {
  state: CellState;
  /** Names the cell for assistive tech, e.g. "Use GitHub". */
  label: string;
  /** Why an inherited cell is ticked, e.g. "Comes from Manage". */
  reason?: string;
  onToggle?: () => void;
}): JSX.Element {
  const interactive =
    !!onToggle &&
    (state === "on" ||
      state === "off" ||
      state === "blocked" ||
      state === "row-blocked");
  const button = (
    <button
      type="button"
      aria-label={
        state === "blocked" || state === "inherited-blocked"
          ? `${label}, blocked`
          : label
      }
      aria-pressed={
        state === "on" || state === "blocked" || state === "row-blocked"
      }
      aria-disabled={!interactive}
      onClick={interactive ? onToggle : undefined}
      className={cn(
        CELL_BASE,
        CELL_STYLE[state],
        interactive ? "cursor-pointer" : "cursor-default",
      )}
    >
      <CellGlyph state={state} />
    </button>
  );
  if (!reason) return button;
  return <SimpleTooltip tooltip={reason}>{button}</SimpleTooltip>;
}

function Legend(): JSX.Element {
  const item = (state: CellState, text: string) => (
    <span className="text-muted-foreground flex items-center gap-2 text-sm">
      <span aria-hidden="true" className={cn(CELL_BASE, CELL_STYLE[state])}>
        <CellGlyph state={state} />
      </span>
      {text}
    </span>
  );
  return (
    <div className="flex flex-wrap gap-x-6 gap-y-2">
      {item("on", "Allowed here")}
      {item("inherited", "Inherited")}
      {item("blocked", "Blocked, even if another role allows it")}
    </div>
  );
}

/**
 * The trailing column's control: what the row covers, opening the editor
 * that narrows it. Disabled rows keep the same shape and say why.
 */
function DropdownChip({
  children,
  label,
  onClick,
  disabledReason,
  tone = "default",
}: {
  children: ReactNode;
  /** Names the control for assistive tech. */
  label: string;
  onClick?: () => void;
  /** Set when the row cannot be narrowed here; shows plain text instead. */
  disabledReason?: string;
  tone?: "default" | "destructive";
}): JSX.Element {
  // Nothing to choose: say what it covers, without a control.
  if (disabledReason) {
    return (
      <SimpleTooltip tooltip={disabledReason}>
        <span className="text-muted-foreground cursor-default text-sm">
          {children}
        </span>
      </SimpleTooltip>
    );
  }
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      className={cn(
        "border-border hover:border-foreground/40 inline-flex h-8 max-w-full cursor-pointer items-center gap-1 border px-2 align-middle text-sm",
        tone === "destructive" && "text-destructive border-destructive/40",
      )}
    >
      <span className="truncate">{children}</span>
      <ChevronDown aria-hidden="true" className="size-3.5 shrink-0" />
    </button>
  );
}

// Eyebrow carries its own colour; a text colour beside it would merge it away.
const TH = "text-eyebrow px-2 py-3 text-center";
const TD = "px-2 py-2 text-center align-middle";

/**
 * A grid of cells with fixed columns: ticking a cell never reflows the
 * table, and the MCP and Platform tables share one set of column widths.
 */
function MatrixTable({
  title,
  columns,
  trailing,
  onReset,
  hideTitle = false,
  children,
}: {
  title: string;
  /** The tab above already names the table; keep the name for screen readers. */
  hideTitle?: boolean;
  /** One per level slot; null leaves the slot empty. */
  columns: ({ label: string; hint: string } | null)[];
  trailing: string;
  /** Empties every cell in the table; disabled when there is nothing. */
  onReset?: () => void;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="bg-card overflow-x-auto">
      <table className="w-full min-w-[44rem] table-fixed border-collapse text-sm">
        <colgroup>
          {/* Proportional, so no column soaks up the spare width: names
              28%, four levels at 12%, the trailing column 24%. */}
          <col className="w-[28%]" />
          {columns.map((_, index) => (
            <col key={index} className="w-[12%]" />
          ))}
          <col className="w-[24%]" />
        </colgroup>
        <thead>
          <tr className="border-border border-b">
            <th scope="col" className={cn(TH, "pl-4 text-left")}>
              <span className={cn(hideTitle && "sr-only")}>{title}</span>
            </th>
            {columns.map((column, index) =>
              column ? (
                <th key={column.label} scope="col" className={TH}>
                  <SimpleTooltip tooltip={column.hint}>
                    <span
                      tabIndex={0}
                      aria-label={`${column.label}: ${column.hint}`}
                      className="inline-flex cursor-default items-center gap-1"
                    >
                      {column.label}
                      <Info aria-hidden="true" className="size-3" />
                    </span>
                  </SimpleTooltip>
                </th>
              ) : (
                <td key={index} aria-hidden="true" />
              ),
            )}
            <th scope="col" className={cn(TH, "pr-4 text-left")}>
              <span className="flex items-center justify-between gap-2">
                {trailing}
                <Button
                  variant="secondary"
                  size="xs"
                  onClick={onReset}
                  disabled={!onReset}
                  className="bg-background normal-case"
                >
                  <Button.LeftIcon>
                    <RotateCcw className="size-3.5" />
                  </Button.LeftIcon>
                  <Button.Text>Reset</Button.Text>
                </Button>
              </span>
            </th>
          </tr>
        </thead>
        <tbody className="divide-border divide-y">{children}</tbody>
      </table>
    </div>
  );
}

function MatrixSkeleton(): JSX.Element {
  return (
    <Skeleton>
      <div className="h-64 w-full" />
    </Skeleton>
  );
}

// ─── MCP servers ────────────────────────────────────────────────────────────

interface McpRowView {
  row: McpRow;
  name: string;
  detail?: string;
  depth: 0 | 1 | 2;
  entry?: ServerWithProject;
}

function rowName(view: McpRowView): string {
  return view.row.kind === "all" ? "all servers" : view.name;
}

function toolLimitText(limit: ToolLimit): string {
  switch (limit.kind) {
    case "all":
      return "All tools";
    case "tools":
      return limit.tools.length === 1
        ? "1 tool"
        : `${limit.tools.length} tools`;
    case "annotations": {
      const labels = limit.dispositions.map((d) => dispositionCopy(d).label);
      if (labels.length === 0) return "No tools";
      return `${labels.join(", ")} tools`;
    }
  }
}

function McpMatrix({
  grants,
  onChange,
  projects,
  inventory,
}: {
  grants: Grants;
  onChange: (change: (prev: Grants) => Grants) => void;
  projects: ProjectRef[];
  inventory: OrgMcpServers;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const [sheet, setSheet] = useState<{
    target: ToolAccessTarget;
    row: McpRow;
    /**
     * The sheet's working choice. Choosing no tools stores no Use at all,
     * which would read back as nothing chosen, so the sheet keeps its own.
     */
    limit: ToolLimit;
  } | null>(null);
  // The default project starts open and the rest collapsed; a search opens
  // them all.
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(
    () =>
      new Set(projects.filter((p) => p.slug === "default").map((p) => p.id)),
  );

  const serverIndex = useMemo(
    () => indexServers(inventory.groups),
    [inventory.groups],
  );
  const serverCount = serverIndex.size;

  const allRows = useMemo((): McpRowView[] => {
    const rows: McpRowView[] = [
      {
        row: { kind: "all" },
        name: "All servers",
        detail: "Including servers added later",
        depth: 0,
      },
    ];
    for (const group of inventory.groups) {
      rows.push({
        row: { kind: "project", projectId: group.projectId },
        name: group.projectName,
        detail:
          group.servers.length === 1
            ? "1 server"
            : `${group.servers.length} servers`,
        depth: 1,
      });
      for (const server of group.servers) {
        rows.push({
          row: {
            kind: "server",
            serverId: server.id,
            projectId: group.projectId,
          },
          name: server.name,
          detail: serverHandle(server),
          depth: 2,
          entry: serverIndex.get(server.id),
        });
      }
    }
    // A project without servers still takes grants for the ones added later.
    const listed = new Set(inventory.groups.map((g) => g.projectId));
    for (const project of projects) {
      if (listed.has(project.id)) continue;
      rows.push({
        row: { kind: "project", projectId: project.id },
        name: project.name,
        detail: "No servers yet",
        depth: 1,
      });
    }
    return rows;
  }, [inventory.groups, serverIndex, projects]);

  // A project row stays while any of its servers match, so a match never
  // loses the project it sits in.
  const visibleRows = useMemo(() => {
    if (!query) {
      return allRows.filter(
        (view) =>
          view.row.kind !== "server" || expanded.has(view.row.projectId),
      );
    }
    const matches = new Set(
      allRows
        .filter(
          (view) =>
            view.row.kind === "server" &&
            fuzzyMatch(query, `${view.name} ${view.detail ?? ""}`),
        )
        .map((view) => (view.row as { projectId: string }).projectId),
    );
    return allRows.filter((view) => {
      if (view.row.kind === "all") return true;
      if (view.row.kind === "project") return matches.has(view.row.projectId);
      return fuzzyMatch(query, `${view.name} ${view.detail ?? ""}`);
    });
  }, [allRows, query, expanded]);

  const other = useMemo(
    () =>
      inventory.settled
        ? otherMcpRules(
            grants,
            allRows.map((view) => view.row),
          )
        : [],
    [grants, allRows, inventory.settled],
  );

  const viewByKey = useMemo(
    () => new Map(allRows.map((view) => [mcpRowKey(view.row), view])),
    [allRows],
  );

  /** The rows above this one, nearest first. */
  const ancestors = (row: McpRow): McpRowView[] => {
    const result: McpRowView[] = [];
    for (let up = parentRow(row); up; up = parentRow(up)) {
      const view = viewByKey.get(mcpRowKey(up));
      if (view) result.push(view);
    }
    return result;
  };

  const cellState = (
    view: McpRowView,
    level: Level,
  ): { state: CellState; reason?: string } => {
    const { row } = view;
    if (mcpBlocks(grants, row, level)) return { state: "blocked" };
    const blockedAbove = ancestors(row).find((up) =>
      mcpBlocks(grants, up.row, level),
    );
    // A row's own grant stays clickable, so it can still be taken back.
    if (mcpAllows(grants, row, level)) {
      return blockedAbove
        ? {
            state: "on",
            reason: `Blocked on ${rowName(blockedAbove)}, so this has no effect`,
          }
        : { state: "on" };
    }
    if (blockedAbove) {
      return {
        state: "inherited-blocked",
        reason: `Blocked on ${rowName(blockedAbove)}`,
      };
    }
    const higher = LEVELS.find(
      (other) =>
        levelRank(other) > levelRank(level) && mcpAllows(grants, row, other),
    );
    if (higher) {
      return {
        state: "inherited",
        reason: `${LEVEL_LABEL[higher]} includes ${LEVEL_LABEL[level]}`,
      };
    }
    for (const up of ancestors(row)) {
      const from = LEVELS.find(
        (other) =>
          levelRank(other) >= levelRank(level) &&
          mcpAllows(grants, up.row, other),
      );
      if (from) {
        return {
          state: "inherited",
          reason: `${LEVEL_LABEL[from]} on ${rowName(up)}`,
        };
      }
    }
    return { state: "off" };
  };

  const blockedState = (
    view: McpRowView,
  ): { state: CellState; reason?: string } => {
    const own = LEVELS.filter((level) => mcpBlocks(grants, view.row, level));
    if (own.length === LEVELS.length) return { state: "row-blocked" };
    const above = ancestors(view.row).find((up) =>
      LEVELS.every((level) => mcpBlocks(grants, up.row, level)),
    );
    if (above) {
      return {
        state: "inherited-blocked",
        reason: `Blocked on ${rowName(above)}`,
      };
    }
    if (own.length > 0) {
      return {
        state: "off",
        reason: `Only ${own.map((l) => LEVEL_LABEL[l]).join(" and ")} is blocked. Click to block everything.`,
      };
    }
    return { state: "off" };
  };

  const toggleCell = (view: McpRowView, level: Level, state: CellState) => {
    onChange((prev) =>
      state === "blocked"
        ? setMcpBlock(prev, view.row, level, false)
        : setMcpAllow(prev, view.row, level, state === "off"),
    );
  };

  const toolsCell = (view: McpRowView): ReactNode => {
    const { row } = view;
    if (blockedState(view).state === "row-blocked") {
      return (
        <SimpleTooltip tooltip="Blocked, even if another role allows it">
          <span className="text-destructive cursor-default text-sm">
            No tools
          </span>
        </SimpleTooltip>
      );
    }
    const states = LEVELS.map((level) => cellState(view, level).state);
    const reaches = states.some(
      (state) => state === "on" || state === "inherited",
    );
    if (!reaches) return <span className="text-muted-foreground">—</span>;
    const limit = mcpToolLimit(grants, row);
    const target: ToolAccessTarget | null =
      row.kind === "all"
        ? { kind: "all" }
        : view.entry
          ? { kind: "server", entry: view.entry }
          : null;
    // A limit narrows this row's own Use. View and Manage reach every tool,
    // and a tick from a broader row reaches them whatever this row says.
    // View and Manage reach every tool. Use from a broader row can still be
    // narrowed on a server: the broader row is split (splitMcpToolLimit).
    const disabledReason = states
      .slice(1)
      .some((state) => state === "on" || state === "inherited")
      ? "View and Manage reach every tool"
      : !target
        ? "Choose tools on each server"
        : undefined;
    return (
      <DropdownChip
        label={`Choose tools for ${rowName(view)}`}
        disabledReason={disabledReason}
        onClick={() => {
          if (target) {
            setSheet({
              target,
              row,
              limit: mcpToolLimit(grants, row) ?? inheritedLimit(view),
            });
          }
        }}
      >
        {disabledReason || states[0] !== "on"
          ? inheritedTools(view)
          : toolLimitText(limit ?? { kind: "all" })}
      </DropdownChip>
    );
  };

  /**
   * The tools a row reaches without its own Use limit: every tool through
   * View or Manage, or whatever the nearest broader row's Use allows.
   */
  const inheritedLimit = (view: McpRowView): ToolLimit => {
    for (const up of [view, ...ancestors(view.row)]) {
      if (
        mcpAllows(grants, up.row, "view") ||
        mcpAllows(grants, up.row, "manage")
      ) {
        return { kind: "all" };
      }
      if (up !== view && mcpAllows(grants, up.row, "use")) {
        return mcpToolLimit(grants, up.row) ?? { kind: "all" };
      }
    }
    return { kind: "all" };
  };
  const inheritedTools = (view: McpRowView) =>
    toolLimitText(inheritedLimit(view));

  const serverRows = allRows.flatMap((view) =>
    view.row.kind === "server" ? [view.row] : [],
  );
  const sheetLimit = sheet?.limit ?? null;

  return (
    <section className="flex flex-col">
      {serverCount > SEARCH_THRESHOLD && (
        <div className="border-border border-b px-4 py-3">
          <SearchBar
            value={query}
            onChange={setQuery}
            placeholder="Search servers"
            className="w-full sm:w-64"
          />
        </div>
      )}

      {inventory.isError && (
        <Alert variant="error" alignTop className="m-4 text-sm">
          <span className="flex flex-wrap items-center gap-2">
            Could not load this organization&rsquo;s MCP servers.
            <Button variant="tertiary" size="xs" onClick={inventory.refetch}>
              <Button.Text>Retry</Button.Text>
            </Button>
          </span>
        </Alert>
      )}

      <MatrixTable
        title="MCP servers"
        hideTitle
        columns={[
          {
            label: "Connect",
            hint: "Connect to the server and call its tools.",
          },
          {
            label: "View",
            hint: "See the server's settings and tool list. Includes Connect.",
          },
          {
            label: "Manage",
            hint: "Change, publish and delete the server. Includes View and Connect.",
          },
          {
            label: "Blocked",
            hint: "Take away every level, even if another role allows it.",
          },
        ]}
        trailing="Tools"
        onReset={
          LEVELS.some((level) => grants[MCP_SCOPE_KEYS[level]])
            ? () => onChange(clearMcp)
            : undefined
        }
      >
        {visibleRows.map((view) => {
          const blocked = blockedState(view);
          return (
            <tr
              key={mcpRowKey(view.row)}
              className={cn(view.depth === 0 && "bg-muted/30")}
            >
              <th
                scope="row"
                className={cn(
                  "py-2 pr-2 text-left font-normal",
                  view.depth === 2 ? "pl-10" : "pl-4",
                )}
              >
                {view.row.kind === "project" ? (
                  <ProjectToggle
                    name={view.name}
                    detail={view.detail}
                    open={!!query || expanded.has(view.row.projectId)}
                    disabled={!!query}
                    onToggle={() => {
                      const id = (view.row as { projectId: string }).projectId;
                      setExpanded((prev) => {
                        const next = new Set(prev);
                        if (!next.delete(id)) next.add(id);
                        return next;
                      });
                    }}
                  />
                ) : (
                  <>
                    <div
                      className={cn(
                        "truncate",
                        view.depth < 2 && "font-medium",
                      )}
                    >
                      {view.name}
                    </div>
                    {view.detail && view.depth < 2 && (
                      <div className="text-muted-foreground text-xs">
                        {view.detail}
                      </div>
                    )}
                  </>
                )}
              </th>
              {LEVELS.map((level) => {
                const { state, reason } = cellState(view, level);
                return (
                  <td key={level} className={TD}>
                    <Cell
                      state={state}
                      reason={reason}
                      label={`${LEVEL_LABEL[level]} ${rowName(view)}`}
                      onToggle={() => toggleCell(view, level, state)}
                    />
                  </td>
                );
              })}
              <td className={TD}>
                <Cell
                  state={blocked.state}
                  reason={blocked.reason}
                  label={`Block ${rowName(view)}`}
                  onToggle={() =>
                    onChange((prev) =>
                      setMcpRowBlocked(
                        prev,
                        view.row,
                        blocked.state !== "row-blocked",
                      ),
                    )
                  }
                />
              </td>
              <td className="truncate py-2 pr-4 pl-2 text-left">
                {toolsCell(view)}
              </td>
            </tr>
          );
        })}
        {!inventory.settled && (
          <tr>
            <td colSpan={6} className="p-4">
              <Skeleton>
                <div className="h-24 w-full" />
              </Skeleton>
            </td>
          </tr>
        )}
        {inventory.settled && inventory.groups.length === 0 && (
          <tr>
            <td colSpan={6} className="text-muted-foreground px-4 py-6 text-sm">
              No MCP servers yet. All servers covers ones you add later.
            </td>
          </tr>
        )}
      </MatrixTable>

      {other.length > 0 && (
        <OtherMcpRules
          rules={other}
          serverIndex={serverIndex}
          projects={projects}
          onRemove={(rule) =>
            onChange((prev) => removeOtherMcpRule(prev, rule))
          }
        />
      )}

      <ToolAccessSheet
        target={sheet?.target ?? null}
        limit={sheetLimit ?? { kind: "all" }}
        initialTab={
          sheetLimit?.kind === "annotations" ? "annotations" : "tools"
        }
        onChange={(limit) => {
          if (!sheet) return;
          const { row } = sheet;
          setSheet({ ...sheet, limit });
          onChange((prev) => limitMcpUse(prev, row, limit, serverRows));
        }}
        onClose={() => setSheet(null)}
      />
    </section>
  );
}

/** A project row's name, which opens and closes its servers. */
function ProjectToggle({
  name,
  detail,
  open,
  disabled,
  onToggle,
}: {
  name: string;
  detail?: string;
  open: boolean;
  /** A search shows every match, so projects stay open while it runs. */
  disabled: boolean;
  onToggle: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={disabled ? undefined : onToggle}
      aria-expanded={open}
      className={cn(
        "-ml-1 flex max-w-full items-start gap-1 text-left",
        disabled ? "cursor-default" : "cursor-pointer",
      )}
    >
      <ChevronRight
        aria-hidden="true"
        className={cn(
          "mt-0.5 size-4 shrink-0 transition-transform",
          open && "rotate-90",
        )}
      />
      <span className="min-w-0">
        <span className="block truncate font-medium">{name}</span>
        {detail && (
          <span className="text-muted-foreground block text-xs">{detail}</span>
        )}
      </span>
    </button>
  );
}

function describeOtherRule(
  rule: OtherMcpRule,
  serverIndex: Map<string, ServerWithProject>,
  projects: ProjectRef[],
): string {
  const { selector: s } = rule;
  const project = s.projectId
    ? (projects.find((p) => p.id === s.projectId)?.name ?? "a removed project")
    : undefined;
  const where =
    s.resourceId === "*"
      ? project
        ? `every server in ${project}`
        : "every server"
      : (serverIndex.get(s.resourceId)?.server.name ?? "a removed server");
  const parts = [where];
  if (s.tool) parts.push(`the ${s.tool} tool`);
  if (s.disposition) {
    parts.push(`${dispositionCopy(s.disposition).label.toLowerCase()} tools`);
  }
  if (s.serverUrl) parts.push(s.serverUrl);
  const verb = rule.blocked ? "Blocks" : "Allows";
  return `${verb} ${LEVEL_LABEL[rule.level]} · ${parts.join(" · ")}`;
}

function OtherMcpRules({
  rules,
  serverIndex,
  projects,
  onRemove,
}: {
  rules: OtherMcpRule[];
  serverIndex: Map<string, ServerWithProject>;
  projects: ProjectRef[];
  onRemove: (rule: OtherMcpRule) => void;
}): JSX.Element {
  return (
    <div className="border-border w-full border-t">
      <div className="border-border border-b px-4 py-3">
        <div className="text-eyebrow text-muted-foreground">Other rules</div>
        <Text muted small>
          Narrower than a row above, or for a server that no longer exists. They
          stay as they are when you save.
        </Text>
      </div>
      <ul className="divide-border divide-y">
        {rules.map((rule) => {
          const text = describeOtherRule(rule, serverIndex, projects);
          return (
            <li
              key={rule.key}
              className="flex items-center justify-between gap-3 px-4 py-2"
            >
              <span
                className={cn("text-sm", rule.blocked && "text-destructive")}
              >
                {text}
              </span>
              <Button
                variant="tertiary"
                size="sm"
                onClick={() => onRemove(rule)}
                aria-label={`Remove rule: ${text}`}
              >
                <Button.LeftIcon>
                  <X className="h-4 w-4" />
                </Button.LeftIcon>
              </Button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

// ─── Platform ───────────────────────────────────────────────────────────────

const PLATFORM_LEVELS = ["view", "manage"] as const;

function PlatformMatrix({
  grants,
  onChange,
  scopes,
  projects,
  markAgentIneligible,
  onEditRule,
  onRemoveRule,
}: {
  grants: Grants;
  onChange: (change: (prev: Grants) => Grants) => void;
  scopes: ScopeDefinition[];
  projects: ProjectRef[];
  markAgentIneligible: boolean;
  onEditRule: (scope: Scope, ruleIndex: number) => void;
  onRemoveRule: (scope: Scope, ruleIndex: number) => void;
}): JSX.Element {
  const areas = useMemo(() => platformAreas(scopes), [scopes]);
  const extras = areas.flatMap((area) =>
    area.extras.map((scope) => ({ area, scope })),
  );

  const cellState = (
    area: PlatformArea,
    level: (typeof PLATFORM_LEVELS)[number],
  ): { state: CellState; reason?: string } | null => {
    const def = area[level];
    if (!def) return null;
    const viewBlocked = platformBlock(grants, area.view?.slug) === "all";
    if (platformBlock(grants, def.slug) === "all") return { state: "blocked" };
    if (level === "manage" && viewBlocked) {
      return {
        state: "inherited-blocked",
        reason: "Blocking View blocks Manage",
      };
    }
    if (platformAllows(grants, def.slug)) return { state: "on" };
    if (
      level === "view" &&
      area.manage &&
      platformAllows(grants, area.manage.slug)
    ) {
      return { state: "inherited", reason: "Manage includes View" };
    }
    return { state: "off" };
  };

  const ineligible = (defs: (ScopeDefinition | undefined)[]) =>
    markAgentIneligible && defs.some((d) => d?.agentEligible === false);

  return (
    <section className="flex flex-col">
      <MatrixTable
        title="Platform feature"
        columns={[
          { label: "View", hint: "See it and its settings." },
          {
            label: "Manage",
            hint: "Create, change and delete it. Includes View.",
          },
          {
            label: "Blocked",
            hint: "Take away every level, even if another role allows it.",
          },
          null,
        ]}
        trailing="Applies to"
        onReset={
          areas.some((area) =>
            [area.view, area.manage, ...area.extras].some(
              (def) => def && grants[def.slug],
            ),
          )
            ? () => onChange((prev) => clearPlatform(prev, areas))
            : undefined
        }
      >
        {areas
          .filter((area) => area.view)
          .map((area) => {
            const blockable = !!(
              area.view?.exclusionScope ?? area.manage?.exclusionScope
            );
            const blocked = platformBlock(grants, area.view?.slug) === "all";
            return (
              <tr key={area.resourceType}>
                <th
                  scope="row"
                  className="py-2 pr-2 pl-4 text-left font-normal"
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <span>{area.label}</span>
                    {ineligible([area.view, area.manage]) && (
                      <Badge variant="neutral" size="sm">
                        Not available to agents
                      </Badge>
                    )}
                  </div>
                  <div className="text-muted-foreground truncate text-xs">
                    {area.description}
                  </div>
                </th>
                {PLATFORM_LEVELS.map((level) => {
                  const cell = cellState(area, level);
                  const def = area[level];
                  return (
                    <td key={level} className={TD}>
                      {cell && def ? (
                        <Cell
                          state={cell.state}
                          reason={cell.reason}
                          label={`${LEVEL_LABEL[level]} ${area.label.toLowerCase()}`}
                          onToggle={() =>
                            onChange((prev) =>
                              cell.state === "blocked"
                                ? unblockPlatformScope(prev, def.slug as Scope)
                                : setPlatformAllow(
                                    prev,
                                    def.slug as Scope,
                                    cell.state === "off",
                                  ),
                            )
                          }
                        />
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                  );
                })}
                <td className={TD}>
                  {blockable ? (
                    <Cell
                      state={blocked ? "row-blocked" : "off"}
                      label={`Block ${area.label.toLowerCase()}`}
                      onToggle={() =>
                        onChange((prev) =>
                          setPlatformBlocked(prev, area, !blocked),
                        )
                      }
                    />
                  ) : (
                    <SimpleTooltip tooltip={`${area.label} cannot be blocked`}>
                      <span className="text-muted-foreground cursor-default">
                        —
                      </span>
                    </SimpleTooltip>
                  )}
                </td>
                {/* Level columns start under Use above; this slot is spare. */}
                <td aria-hidden="true" />
                <td className="py-2 pr-4 pl-2 text-left">
                  <AppliesTo
                    grants={grants}
                    defs={[area.view, area.manage]}
                    resourceType={area.resourceType}
                    projects={projects}
                    onEditRule={onEditRule}
                    onRemoveRule={onRemoveRule}
                  />
                </td>
              </tr>
            );
          })}
        {extras.map(({ area, scope }) => {
          const checked = platformAllows(grants, scope.slug);
          const blockedEverywhere = platformBlock(grants, scope.slug) === "all";
          const copy = extraCopy(scope);
          return (
            <tr key={scope.slug}>
              <th scope="row" className="py-2 pr-2 pl-4 text-left font-normal">
                <div className="flex flex-wrap items-center gap-2">
                  <span>{copy.label}</span>
                  {ineligible([scope]) && (
                    <Badge variant="neutral" size="sm">
                      Not available to agents
                    </Badge>
                  )}
                </div>
                <div className="text-muted-foreground truncate text-xs">
                  {copy.description}
                </div>
              </th>
              <td className={TD}>
                <Cell
                  state={blockedEverywhere ? "blocked" : checked ? "on" : "off"}
                  label={copy.label}
                  onToggle={() =>
                    onChange((prev) =>
                      blockedEverywhere
                        ? unblockPlatformScope(prev, scope.slug as Scope)
                        : setPlatformAllow(prev, scope.slug as Scope, !checked),
                    )
                  }
                />
              </td>
              <td colSpan={3} aria-hidden="true" />
              <td className="py-2 pr-4 pl-2 text-left">
                <AppliesTo
                  grants={grants}
                  defs={[scope]}
                  resourceType={area.resourceType}
                  projects={projects}
                  onEditRule={onEditRule}
                  onRemoveRule={onRemoveRule}
                />
              </td>
            </tr>
          );
        })}
      </MatrixTable>
    </section>
  );
}

/**
 * Where a platform permission applies: one link per rule, which opens the
 * rule editor to narrow it to projects. A block on some projects carries its
 * own remove button, since the Blocked column only blocks everywhere.
 */
function AppliesTo({
  grants,
  defs,
  resourceType,
  projects,
  onEditRule,
  onRemoveRule,
}: {
  grants: Grants;
  defs: (ScopeDefinition | undefined)[];
  resourceType: ScopeDefinition["resourceType"];
  projects: ProjectRef[];
  onEditRule: (scope: Scope, ruleIndex: number) => void;
  onRemoveRule: (scope: Scope, ruleIndex: number) => void;
}): JSX.Element | null {
  const type = resourceType as Parameters<typeof computeRuleLabel>[1];
  const links = defs.flatMap((def, position) => {
    if (!def) return [];
    return (grants[def.slug]?.rules ?? []).map((rule, index) => ({
      key: `${def.slug}:${rule.id}`,
      scope: def.slug as Scope,
      index,
      blocked: rule.effect === "deny",
      level: position === 0 ? "View" : "Manage",
      label: computeRuleLabel(rule.selectors, type, projects),
      everywhere: rule.selectors === null,
    }));
  });
  // A block everywhere is the Blocked column; listing it again says nothing.
  const shown = links.filter((link) => !(link.blocked && link.everywhere));
  if (shown.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  if (!isProjectScopedResourceType(type)) {
    return (
      <DropdownChip
        label="Applies to the whole organization"
        disabledReason="Organization permissions cannot be narrowed"
      >
        Whole organization
      </DropdownChip>
    );
  }
  // The same place granted at both levels reads once; different places say
  // which level each is for.
  const allows = shown.filter((link) => !link.blocked);
  const levelled = new Set(allows.map((link) => link.label)).size > 1;
  const seen = new Set<string>();
  const unique = shown.filter((link) => {
    const id = link.blocked || levelled ? link.key : link.label;
    if (seen.has(id)) return false;
    seen.add(id);
    return true;
  });
  return (
    <span className="flex flex-wrap items-center gap-1">
      {unique.map((link) => (
        <span
          key={link.key}
          className="inline-flex max-w-full items-center gap-1"
        >
          <DropdownChip
            label={`Change where ${link.blocked ? "the block" : link.level} applies`}
            tone={link.blocked ? "destructive" : "default"}
            onClick={() => onEditRule(link.scope, link.index)}
          >
            {link.blocked ? "Blocked: " : levelled ? `${link.level}: ` : ""}
            {link.label}
          </DropdownChip>
          {link.blocked && (
            <button
              type="button"
              onClick={() => onRemoveRule(link.scope, link.index)}
              aria-label={`Remove block on ${link.label}`}
              className="text-muted-foreground hover:text-foreground cursor-pointer p-1"
            >
              <X aria-hidden="true" className="size-3.5" />
            </button>
          )}
        </span>
      ))}
    </span>
  );
}
