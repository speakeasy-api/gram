import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { SearchBar } from "@/components/ui/SearchBar";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import { ChevronRight, Info, MoreHorizontal, X } from "lucide-react";
import { Fragment, useMemo, useState, type JSX, type ReactNode } from "react";

import {
  DISPOSITIONS,
  dispositionCopy,
  fuzzyMatch,
  indexServers,
  toolDisposition,
  type ServerWithProject,
  type ToolLimit,
} from "./mcpAccessModel";
import { DenyDialog, ExplicitDenies } from "./RoleDenies";
import {
  excludeMcpRow,
  LEVEL_LABEL,
  LEVELS,
  type Level,
  limitMcpUse,
  type McpRow,
  type McpRowView,
  mcpAllows,
  mcpBlockedTools,
  mcpBlocks,
  mcpLevelIncludes,
  mcpRowDenied,
  mcpRowKey,
  mcpRowViews,
  mcpToolLimit,
  type OtherMcpRule,
  otherMcpRules,
  parentRow,
  platformAreas,
  removeOtherMcpRule,
  roleDenies,
  setMcpAllow,
  setMcpBlock,
  setMcpRowBlocked,
  setMcpToolBlocked,
} from "./roleMatrixModel";
import {
  Cell,
  type CellState,
  DeniedBadge,
  type DenyTarget,
  EditorSection,
  LegendCell,
  type Grants,
  type ProjectRef,
} from "./roleMatrixParts";
import { RolePlatformCards } from "./RolePlatformCards";
import type { OrgMcpServers } from "./useOrgMcpServers";
import { useServerTools } from "./useServerTools";

/** Above this many servers the table gets a search box. */
const SEARCH_THRESHOLD = 8;
/**
 * How many projects the grid lists before the rest fold behind a "more"
 * row, so a long list does not push Platform Access far down the page.
 */
const PROJECT_CLIP = 10;

const DENY_HINT =
  "Rarely needed. Wins over every grant, including direct ones.";
const REMOVE_DENY_HINT = "Back to not granted by this role.";

/**
 * What a role can do, as two sections of one page. MCP access is a grid of
 * servers by Connect / View / Manage: ticks the role stores are solid, ticks
 * it gets from a higher level or a wider row are dashed. Platform access is a
 * card per area with one level each. A deny is never a level: it is the last
 * item of a row's or card's menu, confirmed in its own dialog and listed in
 * one place below.
 */
export function RoleAccessMatrix({
  grants,
  onChange,
  scopes,
  projects,
  inventory,
  loading,
  markAgentIneligible,
  memberCount,
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
  /** How many members hold the role, for the deny dialog's warning. */
  memberCount: number;
  /** Narrow one rule of a platform permission to particular projects. */
  onEditRule: (scope: Scope, ruleIndex: number) => void;
  /** Take back one rule of a platform permission. */
  onRemoveRule: (scope: Scope, ruleIndex: number) => void;
}): JSX.Element {
  const [denyTarget, setDenyTarget] = useState<DenyTarget | null>(null);
  const areas = useMemo(() => platformAreas(scopes), [scopes]);
  const rowViews = useMemo(
    () => mcpRowViews(inventory.groups, projects),
    [inventory.groups, projects],
  );
  const denies = useMemo(
    () =>
      roleDenies(
        grants,
        inventory.settled ? rowViews.map((view) => view.row) : null,
        areas,
      ),
    [grants, inventory.settled, rowViews, areas],
  );

  return (
    <div className="flex flex-col gap-12">
      {loading ? (
        <MatrixSkeleton />
      ) : (
        <>
          <McpMatrix
            grants={grants}
            onChange={onChange}
            scopes={scopes}
            projects={projects}
            inventory={inventory}
            rowViews={rowViews}
            onDeny={setDenyTarget}
          />
          <RolePlatformCards
            grants={grants}
            onChange={onChange}
            areas={areas}
            projects={projects}
            markAgentIneligible={markAgentIneligible}
            onEditRule={onEditRule}
            onRemoveRule={onRemoveRule}
            onDeny={setDenyTarget}
          />
          <ExplicitDenies
            denies={denies}
            rowViews={rowViews}
            projects={projects}
            onChange={onChange}
            onRemoveRule={onRemoveRule}
          />
        </>
      )}
      <DenyDialog
        target={denyTarget}
        memberCount={memberCount}
        onConfirm={(target) => {
          onChange(target.apply);
          setDenyTarget(null);
        }}
        onClose={() => setDenyTarget(null)}
      />
    </div>
  );
}

function Legend(): JSX.Element {
  const item = (state: CellState, text: string) => (
    <span className="text-muted-foreground flex items-center gap-2 text-sm">
      <LegendCell state={state} />
      {text}
    </span>
  );
  return (
    <div className="flex flex-wrap gap-x-6 gap-y-2">
      {item("on", "Allowed here")}
      {item("inherited", "Inherited from a wider row")}
    </div>
  );
}

// Eyebrow carries its own colour; a text colour beside it would merge it away.
const TH = "text-eyebrow px-2 py-3 text-center";
const TD = "px-2 py-2 text-center align-middle";
/** Name, three levels, tools, the row menu. */
const COLUMN_COUNT = 6;

function MatrixSkeleton(): JSX.Element {
  return (
    <Skeleton>
      <div className="h-64 w-full" />
    </Skeleton>
  );
}

// ─── MCP servers ────────────────────────────────────────────────────────────

function rowName(view: McpRowView): string {
  return view.row.kind === "all" ? "all servers" : view.name;
}

/** Where a deny on this row applies, as the deny dialog words it. */
function rowWhere(view: McpRowView): string {
  switch (view.row.kind) {
    case "all":
      return "all servers";
    case "project":
      return `every server in ${view.name}`;
    case "server":
      return view.name;
  }
}

function toolLimitText(limit: ToolLimit): string {
  switch (limit.kind) {
    case "all":
      return "All tools";
    case "tools":
      if (limit.tools.length === 0) return "No tools";
      return limit.tools.length === 1
        ? "1 tool"
        : `${limit.tools.length} tools`;
    case "annotations": {
      const selected = new Set(limit.dispositions);
      const ordered = DISPOSITIONS.filter((value) => selected.delete(value));
      const labels = [...ordered, ...selected].map(
        (d) => dispositionCopy(d).label,
      );
      if (labels.length === 0) return "No tools";
      return `${labels.join(", ")} tools`;
    }
  }
}

/** How a server reaches its tools, for the tool rows under it. */
interface ToolAccess {
  /** View or Manage reaches the server, and with it every tool. */
  connect: "own" | "inherited" | "none";
  /** The wider row an inherited Connect comes from. */
  from?: string;
  /** The tools Connect is limited to. */
  limit: ToolLimit;
}

function McpMatrix({
  grants,
  onChange,
  scopes,
  projects,
  inventory,
  rowViews,
  onDeny,
}: {
  grants: Grants;
  onChange: (change: (prev: Grants) => Grants) => void;
  scopes: ScopeDefinition[];
  projects: ProjectRef[];
  inventory: OrgMcpServers;
  rowViews: McpRowView[];
  onDeny: (target: DenyTarget) => void;
}): JSX.Element {
  // What each level includes is the server's to say (sub_scopes), so the
  // column hints follow the catalogue rather than restate it.
  const columnHint = (level: Level, base: string) => {
    const included = mcpLevelIncludes(scopes, level)
      .map((other) => LEVEL_LABEL[other])
      .reverse();
    return included.length > 0
      ? `${base} Includes ${included.join(" and ")}.`
      : base;
  };
  const columns: { level: Level; hint: string }[] = [
    {
      level: "use",
      hint: columnHint("use", "Connect to the server and call its tools."),
    },
    {
      level: "view",
      hint: columnHint("view", "See the server's settings and tool list."),
    },
    {
      level: "manage",
      hint: columnHint("manage", "Change, publish and delete the server."),
    },
  ];
  const [query, setQuery] = useState("");
  // Every project starts collapsed, so the grid stays short; a search opens
  // them all.
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  /** Servers whose tools are unfolded under them. */
  const [openServers, setOpenServers] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  /**
   * Rows whose Connect stays ticked with no tool chosen yet. Connect with no
   * tools stores nothing, so the tick would otherwise vanish the moment
   * "All tools" is unticked, before any tool could be picked.
   */
  const [connectNoTools, setConnectNoTools] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  const setNoTools = (row: McpRow, on: boolean) =>
    setConnectNoTools((prev) => {
      const key = mcpRowKey(row);
      if (prev.has(key) === on) return prev;
      const next = new Set(prev);
      if (on) next.add(key);
      else next.delete(key);
      return next;
    });
  const toggleIn = (
    set: (update: (prev: ReadonlySet<string>) => ReadonlySet<string>) => void,
    id: string,
  ) =>
    set((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  const serverIndex = useMemo(
    () => indexServers(inventory.groups),
    [inventory.groups],
  );
  const serverCount = serverIndex.size;

  /** A long project list shows its first few until "more" is pressed. */
  const [showAllProjects, setShowAllProjects] = useState(false);
  const projectIds = useMemo(
    () =>
      rowViews.flatMap((view) =>
        view.row.kind === "project" ? [view.row.projectId] : [],
      ),
    [rowViews],
  );
  const clipped = projectIds.length > PROJECT_CLIP;
  const hiddenProjects =
    clipped && !showAllProjects && !query
      ? projectIds.length - PROJECT_CLIP
      : 0;

  // A project row stays while any of its servers match, so a match never
  // loses the project it sits in.
  const visibleRows = useMemo(() => {
    // The grid starts at projects. A grant on every server is still read
    // (its ticks show dashed on the rows under it) and is listed under the
    // table, where it can be taken back.
    if (!query) {
      // A search reaches every project, so only the plain list is clipped.
      const shown = new Set(
        showAllProjects ? projectIds : projectIds.slice(0, PROJECT_CLIP),
      );
      return rowViews.filter((view) =>
        view.row.kind === "server"
          ? shown.has(view.row.projectId) && expanded.has(view.row.projectId)
          : view.row.kind === "project" && shown.has(view.row.projectId),
      );
    }
    const matches = new Set(
      rowViews
        .filter(
          (view) =>
            view.row.kind === "server" &&
            fuzzyMatch(query, `${view.name} ${view.detail ?? ""}`),
        )
        .map((view) => (view.row as { projectId: string }).projectId),
    );
    return rowViews.filter((view) => {
      if (view.row.kind === "all") return false;
      if (view.row.kind === "project") return matches.has(view.row.projectId);
      return fuzzyMatch(query, `${view.name} ${view.detail ?? ""}`);
    });
  }, [rowViews, query, expanded, showAllProjects, projectIds]);

  // Blocks are listed with the role's other denies; only grants the grid has
  // no cell for are left to list here.
  const other = useMemo(
    () =>
      inventory.settled
        ? otherMcpRules(
            grants,
            rowViews.map((view) => view.row),
          ).filter((rule) => !rule.blocked)
        : [],
    [grants, rowViews, inventory.settled],
  );

  // Grants on every server have no row of their own.
  const everyServer: McpRow = { kind: "all" };
  const otherItems: OtherRuleItem[] = [
    ...LEVELS.filter((level) => mcpAllows(grants, everyServer, level)).map(
      (level) => {
        const limit =
          level === "use" ? mcpToolLimit(grants, everyServer) : null;
        const tools =
          limit && limit.kind !== "all" ? ` · ${toolLimitText(limit)}` : "";
        return {
          key: `every:${level}`,
          text: `Allows ${LEVEL_LABEL[level]} · every server, including ones added later${tools}`,
          onRemove: () =>
            onChange((prev) => setMcpAllow(prev, everyServer, level, false)),
        };
      },
    ),
    ...other.map((rule) => ({
      key: rule.key,
      text: describeOtherRule(rule, serverIndex, projects),
      onRemove: () => onChange((prev) => removeOtherMcpRule(prev, rule)),
    })),
  ];

  const viewByKey = useMemo(
    () => new Map(rowViews.map((view) => [mcpRowKey(view.row), view])),
    [rowViews],
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

  /** Where the row is denied from: itself, or the nearest wider row. */
  const deniedOn = (view: McpRowView): McpRowView | undefined =>
    [view, ...ancestors(view.row)].find((up) => mcpRowDenied(grants, up.row));

  /** Connect ticked on the row with no tools behind it yet: nothing stored. */
  const connectOnly = (row: McpRow) =>
    connectNoTools.has(mcpRowKey(row)) && !mcpAllows(grants, row, "use");

  const cellState = (
    view: McpRowView,
    level: Level,
  ): {
    state: CellState;
    reason?: string;
    locked?: boolean;
    /** Inherited at this same level from a wider row, so it can be split. */
    handedDown?: boolean;
  } => {
    const { row } = view;
    if (mcpBlocks(grants, row, level)) {
      // A denied row is taken back from its menu, not one level at a time.
      return mcpRowDenied(grants, row)
        ? {
            state: "blocked",
            reason: "Denied on this row. Remove the deny from the row's menu.",
            locked: true,
          }
        : { state: "blocked" };
    }
    const blockedAbove = ancestors(row).find((up) =>
      mcpBlocks(grants, up.row, level),
    );
    // A row's own grant stays clickable, so it can still be taken back.
    const noTools =
      level === "use" &&
      connectNoTools.has(mcpRowKey(row)) &&
      !mcpAllows(grants, row, level);
    if (noTools) {
      return {
        state: "on",
        reason: "No tools chosen yet, so this grants nothing until one is",
      };
    }
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
    // Whether a grant of `other` satisfies this level. The server says what
    // each permission includes (sub_scopes): Manage includes View, and
    // neither includes Connect.
    const includes = (other: Level) =>
      other === level || mcpLevelIncludes(scopes, other).includes(level);
    const higher = LEVELS.find(
      (other) =>
        other !== level && includes(other) && mcpAllows(grants, row, other),
    );
    if (higher) {
      return {
        state: "inherited",
        reason: `${LEVEL_LABEL[higher]} includes ${LEVEL_LABEL[level]}`,
      };
    }
    for (const up of ancestors(row)) {
      const from = LEVELS.find(
        (other) => includes(other) && mcpAllows(grants, up.row, other),
      );
      if (from) {
        // The same level handed down from a wider row can be unticked here:
        // the wider grant is split to leave this row out.
        // Only when nothing else would still give the level: View that also
        // comes from Manage, here or above, has to be taken back there.
        const alsoCovered = [view, ...ancestors(row)].some((covering) =>
          LEVELS.some(
            (other) =>
              other !== level &&
              includes(other) &&
              mcpAllows(grants, covering.row, other),
          ),
        );
        const handedDown = from === level && !alsoCovered;
        return {
          state: "inherited",
          reason: handedDown
            ? `${LEVEL_LABEL[from]} on ${rowName(up)}. Untick to leave ${rowName(view)} out.`
            : `${LEVEL_LABEL[from]} on ${rowName(up)}`,
          handedDown,
        };
      }
    }
    if (level === "use") {
      const pending = ancestors(row).find((up) => connectOnly(up.row));
      if (pending) {
        return {
          state: "inherited",
          reason: `Connect on ${rowName(pending)}, with no tools chosen yet`,
        };
      }
    }
    return { state: "off" };
  };

  const reaches = (state: CellState) => state === "on" || state === "inherited";

  const toggleCell = (view: McpRowView, level: Level, state: CellState) => {
    if (level === "use") setNoTools(view.row, false);
    onChange((prev) =>
      state === "blocked"
        ? setMcpBlock(prev, view.row, level, false)
        : setMcpAllow(prev, view.row, level, state === "off"),
    );
  };

  /**
   * The tools a row reaches without its own Connect limit: every tool through
   * View or Manage, or whatever the nearest wider row's Connect allows.
   */
  const inheritedLimit = (view: McpRowView): ToolLimit => {
    for (const up of ancestors(view.row)) {
      if (mcpAllows(grants, up.row, "use")) {
        return mcpToolLimit(grants, up.row) ?? { kind: "all" };
      }
    }
    return { kind: "all" };
  };

  const toolAccess = (view: McpRowView): ToolAccess => {
    // Only Connect calls tools; View and Manage reach none of them.
    if (mcpAllows(grants, view.row, "use")) {
      return {
        connect: "own",
        limit: mcpToolLimit(grants, view.row) ?? { kind: "all" },
      };
    }
    if (connectNoTools.has(mcpRowKey(view.row))) {
      return { connect: "own", limit: { kind: "tools", tools: [] } };
    }
    const from = ancestors(view.row).find((up) =>
      mcpAllows(grants, up.row, "use"),
    );
    if (from) {
      return {
        connect: "inherited",
        from: rowName(from),
        limit: inheritedLimit(view),
      };
    }
    const pending = ancestors(view.row).find((up) => connectOnly(up.row));
    return pending
      ? {
          connect: "inherited",
          from: rowName(pending),
          limit: { kind: "tools", tools: [] },
        }
      : { connect: "none", limit: { kind: "all" } };
  };

  const toolsCell = (view: McpRowView): ReactNode => {
    const denied = deniedOn(view);
    if (denied) {
      return (
        <span className="flex flex-wrap items-center gap-2">
          <DeniedBadge />
          <span className="text-muted-foreground text-xs">
            {denied === view
              ? `on ${rowWhere(view)}`
              : `blocked on ${rowName(denied)}`}
          </span>
        </span>
      );
    }
    const { row } = view;
    const access = toolAccess(view);
    // The tick is "every tool". Connect from a wider row can still be
    // narrowed on a server, which splits the wider row (splitMcpToolLimit).
    // A row with no Connect at all still offers the tick: it grants Connect
    // on every tool in one step.
    const every = access.connect !== "none" && access.limit.kind === "all";
    let state: CellState = "off";
    let reason: string | undefined;
    if (every) {
      state = access.connect === "own" ? "on" : "inherited";
      reason =
        access.connect === "own"
          ? undefined
          : `Connect on ${access.from}. Untick to leave this server out.`;
    } else {
      reason = "Tick to allow every tool";
    }
    return (
      <span className="flex items-center gap-3">
        <Cell
          state={state}
          reason={reason}
          narrowable
          label={`All tools on ${rowName(view)}`}
          onToggle={() => {
            // A project that gets Connect from every server: split that
            // grant to leave the project out, keeping its Connect ticked.
            if (every && row.kind === "project" && access.connect !== "own") {
              setNoTools(row, true);
              onChange((prev) => excludeMcpRow(prev, row, "use", matrixRows));
              return;
            }
            // Connect stays ticked; the tools are then picked one by
            // one under the server.
            setNoTools(row, every);
            if (every && row.kind === "server") {
              setOpenServers((prev) => new Set(prev).add(row.serverId));
            }
            onChange((prev) =>
              limitMcpUse(
                prev,
                row,
                every ? { kind: "tools", tools: [] } : { kind: "all" },
                serverRows,
              ),
            );
          }}
        />
        <span className={cn("truncate", !every && "text-muted-foreground")}>
          {every ? "All tools" : toolLimitText(access.limit)}
        </span>
      </span>
    );
  };

  const rowActions = (view: McpRowView): Action[] => {
    const { row } = view;
    const actions: Action[] = [];
    if (row.kind === "server") {
      const open = openServers.has(row.serverId);
      actions.push({
        label: open ? "Hide tools" : "Choose tools…",
        description: `Limit Connect on ${view.name} to some tools.`,
        onClick: () => toggleIn(setOpenServers, row.serverId),
      });
    }
    const denied = deniedOn(view);
    if (!denied) {
      actions.push({
        label: "Deny explicitly…",
        description: DENY_HINT,
        destructive: true,
        separatorBefore: actions.length > 0,
        onClick: () =>
          onDeny({
            what: "MCP access",
            where: rowWhere(view),
            apply: (prev) => setMcpRowBlocked(prev, row, true),
          }),
      });
    } else if (denied === view) {
      actions.push({
        label: "Remove deny",
        description: REMOVE_DENY_HINT,
        destructive: true,
        separatorBefore: actions.length > 0,
        onClick: () => onChange((prev) => setMcpRowBlocked(prev, row, false)),
      });
    }
    return actions;
  };

  const matrixRows = rowViews.map((view) => view.row);
  const serverRows = rowViews.flatMap((view) =>
    view.row.kind === "server" ? [view.row] : [],
  );

  const serverViews = rowViews.filter((view) => view.row.kind === "server");
  const reachable = serverViews.filter(
    (view) =>
      !deniedOn(view) &&
      // Only Connect reaches a server's tools, and Connect with no tools
      // behind it reaches nothing yet.
      reaches(cellState(view, "use").state) &&
      (() => {
        const { limit } = toolAccess(view);
        return !(limit.kind === "tools" && limit.tools.length === 0);
      })(),
  ).length;
  const all = viewByKey.get("all");
  const count =
    all && mcpRowDenied(grants, all.row)
      ? "Denied on all servers"
      : serverViews.length === 0
        ? undefined
        : `${reachable} of ${serverViews.length} ${
            serverViews.length === 1 ? "server" : "servers"
          } reachable`;

  return (
    <EditorSection
      title="MCP Access"
      description="Which servers the role can connect to, see or manage. A level on a wider row reaches every server under it."
      count={count}
    >
      <Legend />
      <div className="border-border bg-card border">
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
          <Alert variant="error" className="m-4 text-sm">
            <span className="flex flex-wrap items-center gap-2">
              Could not load this organization&rsquo;s MCP servers.
              <Button variant="tertiary" size="xs" onClick={inventory.refetch}>
                <Button.Text>Retry</Button.Text>
              </Button>
            </span>
          </Alert>
        )}

        <div className="overflow-x-auto">
          <table className="w-full min-w-[44rem] table-fixed border-collapse text-sm">
            <colgroup>
              <col className="w-[30%]" />
              {columns.map((column) => (
                <col key={column.level} className="w-[12%]" />
              ))}
              <col />
              <col className="w-12" />
            </colgroup>
            <thead>
              <tr className="border-border bg-muted/40 border-b">
                <th scope="col" className={cn(TH, "pl-4 text-left")}>
                  MCP servers
                </th>
                {columns.map((column) => (
                  <th key={column.level} scope="col" className={TH}>
                    <SimpleTooltip tooltip={column.hint}>
                      <span
                        tabIndex={0}
                        aria-label={`${LEVEL_LABEL[column.level]}: ${column.hint}`}
                        className="inline-flex cursor-default items-center gap-1"
                      >
                        {LEVEL_LABEL[column.level]}
                        <Info aria-hidden="true" className="size-3" />
                      </span>
                    </SimpleTooltip>
                  </th>
                ))}
                <th scope="col" className={cn(TH, "text-left")}>
                  Tools
                </th>
                <th scope="col" className={TH}>
                  <span className="sr-only">More</span>
                </th>
              </tr>
            </thead>
            <tbody className="divide-border divide-y">
              {visibleRows.map((view) => {
                const { row } = view;
                const denied = deniedOn(view);
                const actions = rowActions(view);
                const open =
                  row.kind === "server" && openServers.has(row.serverId);
                const entry =
                  row.kind === "server"
                    ? serverIndex.get(row.serverId)
                    : undefined;
                return (
                  <Fragment key={mcpRowKey(row)}>
                    <tr className={cn(view.depth === 0 && "bg-muted/30")}>
                      <th
                        scope="row"
                        className={cn(
                          "py-2 pr-2 text-left font-normal",
                          view.depth === 2 ? "pl-10" : "pl-4",
                          denied === view && "border-l-destructive border-l-2",
                        )}
                      >
                        {row.kind === "project" ? (
                          <RowToggle
                            name={view.name}
                            detail={view.detail}
                            strong
                            open={!!query || expanded.has(row.projectId)}
                            disabled={!!query}
                            onToggle={() =>
                              toggleIn(setExpanded, row.projectId)
                            }
                          />
                        ) : row.kind === "server" ? (
                          <RowToggle
                            name={view.name}
                            label={`${open ? "Hide" : "Show"} tools of ${view.name}`}
                            open={open}
                            disabled={false}
                            onToggle={() =>
                              toggleIn(setOpenServers, row.serverId)
                            }
                          />
                        ) : (
                          <>
                            <div className="truncate font-medium">
                              {view.name}
                            </div>
                            <div className="text-muted-foreground text-xs">
                              {view.detail}
                            </div>
                          </>
                        )}
                      </th>
                      {LEVELS.map((level) => {
                        const { state, reason, locked, handedDown } = cellState(
                          view,
                          level,
                        );
                        return (
                          <td key={level} className={TD}>
                            <Cell
                              state={state}
                              reason={reason}
                              label={`${LEVEL_LABEL[level]} ${rowName(view)}`}
                              narrowable={handedDown}
                              onToggle={
                                locked
                                  ? undefined
                                  : handedDown
                                    ? () =>
                                        onChange((prev) =>
                                          excludeMcpRow(
                                            prev,
                                            row,
                                            level,
                                            matrixRows,
                                          ),
                                        )
                                    : () => toggleCell(view, level, state)
                              }
                            />
                          </td>
                        );
                      })}
                      <td className="truncate py-2 pr-2 pl-2 text-left">
                        {toolsCell(view)}
                      </td>
                      <td className={TD}>
                        {actions.length > 0 && (
                          <MoreActions
                            actions={actions}
                            triggerAriaLabel={`More for ${rowName(view)}`}
                          />
                        )}
                      </td>
                    </tr>
                    {open && row.kind === "server" && entry && (
                      <ServerToolRows
                        view={view}
                        row={row}
                        entry={entry}
                        grants={grants}
                        access={toolAccess(view)}
                        deniedOn={denied ? rowName(denied) : undefined}
                        serverRows={serverRows}
                        onNoTools={(on) => setNoTools(row, on)}
                        onChange={onChange}
                        onDeny={onDeny}
                      />
                    )}
                  </Fragment>
                );
              })}
              {clipped && !query && (
                <tr>
                  <td colSpan={COLUMN_COUNT} className="px-4 py-2">
                    <button
                      type="button"
                      aria-expanded={showAllProjects}
                      onClick={() => setShowAllProjects((prev) => !prev)}
                      className="text-muted-foreground hover:text-foreground -ml-1 inline-flex cursor-pointer items-center gap-2 text-sm"
                    >
                      <MoreHorizontal aria-hidden="true" className="size-4" />
                      {showAllProjects
                        ? "Show fewer projects"
                        : `Show ${hiddenProjects} more ${
                            hiddenProjects === 1 ? "project" : "projects"
                          }`}
                    </button>
                  </td>
                </tr>
              )}
              {!inventory.settled && (
                <tr>
                  <td colSpan={COLUMN_COUNT} className="p-4">
                    <Skeleton>
                      <div className="h-24 w-full" />
                    </Skeleton>
                  </td>
                </tr>
              )}
              {inventory.settled && inventory.groups.length === 0 && (
                <tr>
                  <td
                    colSpan={COLUMN_COUNT}
                    className="text-muted-foreground px-4 py-6 text-sm"
                  >
                    No MCP servers yet. A tick on a project covers servers added
                    to it later.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {otherItems.length > 0 && <OtherMcpRules items={otherItems} />}
      </div>
      <Text muted small>
        Grants add up: a tick on a project reaches every server in it, now and
        later.
      </Text>
    </EditorSection>
  );
}

/** A row's name, which opens and closes what sits under it. */
function RowToggle({
  name,
  detail,
  label,
  strong = false,
  open,
  disabled,
  onToggle,
}: {
  name: string;
  detail?: string;
  /** Names the control when the row's name alone would not say what opens. */
  label?: string;
  strong?: boolean;
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
      aria-label={label}
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
        <span className={cn("block truncate", strong && "font-medium")}>
          {name}
        </span>
        {detail && (
          <span className="text-muted-foreground block text-xs">{detail}</span>
        )}
      </span>
    </button>
  );
}

/**
 * A server's tools, one row each under it. A tool's Connect cell narrows the
 * server's Connect to the tools left ticked; a tool can also be denied on its
 * own from its menu.
 */
function ServerToolRows({
  view,
  row,
  entry,
  grants,
  access,
  deniedOn,
  serverRows,
  onNoTools,
  onChange,
  onDeny,
}: {
  view: McpRowView;
  row: McpRow & { kind: "server" };
  entry: ServerWithProject;
  grants: Grants;
  access: ToolAccess;
  /** The row the server is denied on, when it is. */
  deniedOn: string | undefined;
  serverRows: (McpRow & { kind: "server" })[];
  /** The last tool was unticked: Connect stays, with no tools. */
  onNoTools: (on: boolean) => void;
  onChange: (change: (prev: Grants) => Grants) => void;
  onDeny: (target: DenyTarget) => void;
}): JSX.Element {
  const source = useServerTools(entry);
  const note = (children: ReactNode) => (
    <tr className="bg-muted/20">
      <td
        colSpan={COLUMN_COUNT}
        className="text-muted-foreground py-2 pr-4 pl-16 text-sm"
      >
        {children}
      </td>
    </tr>
  );
  switch (source.status) {
    case "loading":
      return note("Loading tools…");
    case "error":
      return note(
        <span className="flex flex-wrap items-center gap-2">
          Couldn&rsquo;t load this server&rsquo;s tools.
          <Button variant="tertiary" size="xs" onClick={source.retry}>
            <Button.Text>Retry</Button.Text>
          </Button>
        </span>,
      );
    case "dynamic":
    case "none":
      return note(
        "This server resolves its tools when they're called, so they can't be listed.",
      );
    case "needs-connect":
      return note(
        <span className="flex flex-wrap items-center gap-2">
          Sign in to this server and its tools are listed here.
          {source.connect && (
            <Button variant="secondary" size="xs" onClick={source.connect}>
              <Button.Text>Connect</Button.Text>
            </Button>
          )}
        </span>,
      );
    case "needs-write":
      return note(
        `Someone who can manage ${view.name} has to connect it once to record its tools.`,
      );
    case "ready":
      break;
  }

  const tools = [...source.tools].sort((a, b) => a.name.localeCompare(b.name));
  if (tools.length === 0) return note("This server has no tools.");

  const { limit } = access;
  const allowed = (tool: (typeof tools)[number]) => {
    if (access.connect === "none") return false;
    if (limit.kind === "all") return true;
    if (limit.kind === "tools") return limit.tools.includes(tool.name);
    const disposition = toolDisposition(tool);
    return !!disposition && limit.dispositions.includes(disposition);
  };
  const blocked = new Set(mcpBlockedTools(grants, row));

  const toggle = (name: string) => {
    const current = tools.filter(allowed).map((tool) => tool.name);
    const next = current.includes(name)
      ? current.filter((tool) => tool !== name)
      : [...current, name];
    const nextLimit: ToolLimit =
      next.length === tools.length
        ? { kind: "all" }
        : { kind: "tools", tools: next };
    onNoTools(next.length === 0);
    onChange((prev) => limitMcpUse(prev, row, nextLimit, serverRows));
  };

  return (
    <>
      {tools.map((tool) => {
        const ownDenied = blocked.has(tool.name);
        const denied = ownDenied || !!deniedOn;
        const disposition = toolDisposition(tool);
        const on = allowed(tool);
        let state: CellState = "off";
        let reason: string | undefined;
        if (denied) {
          state = ownDenied ? "blocked" : "inherited-blocked";
          reason = ownDenied
            ? "Denied. Remove the deny from the tool's menu."
            : `Blocked on ${deniedOn}`;
        } else if (on) {
          state = access.connect === "own" ? "on" : "inherited";
          reason =
            access.connect === "own"
              ? undefined
              : `Connect on ${access.from}. Untick to leave this tool out.`;
        }
        const actions: Action[] = ownDenied
          ? [
              {
                label: "Remove deny",
                description: REMOVE_DENY_HINT,
                destructive: true,
                onClick: () =>
                  onChange((prev) =>
                    setMcpToolBlocked(prev, row, tool.name, false),
                  ),
              },
            ]
          : deniedOn
            ? []
            : [
                {
                  label: "Deny explicitly…",
                  description: DENY_HINT,
                  destructive: true,
                  onClick: () =>
                    onDeny({
                      what: `the ${tool.name} tool`,
                      where: view.name,
                      apply: (prev) =>
                        setMcpToolBlocked(prev, row, tool.name, true),
                    }),
                },
              ];
        return (
          <tr key={tool.id} className="bg-muted/20">
            <th
              scope="row"
              className={cn(
                "py-1.5 pr-2 pl-16 text-left font-normal",
                ownDenied && "border-l-destructive border-l-2",
              )}
            >
              <span className="flex min-w-0 items-center gap-2">
                <Text as="span" mono small className="min-w-0 truncate">
                  {tool.name}
                </Text>
                {disposition && (
                  <Badge variant="neutral" size="sm">
                    {dispositionCopy(disposition).label}
                  </Badge>
                )}
              </span>
            </th>
            <td className={TD}>
              <span className="text-muted-foreground">—</span>
            </td>
            <td className={TD}>
              <span className="text-muted-foreground">—</span>
            </td>
            <td className={TD}>
              <span className="text-muted-foreground">—</span>
            </td>
            <td className="py-1.5 pr-2 pl-2 text-left">
              <span className="flex flex-wrap items-center gap-2">
                <Cell
                  state={state}
                  reason={reason}
                  narrowable
                  label={`Connect ${tool.name} on ${view.name}`}
                  onToggle={denied ? undefined : () => toggle(tool.name)}
                />
                {denied && (
                  <>
                    <DeniedBadge />
                    <span className="text-muted-foreground text-xs">
                      {ownDenied ? `on ${tool.name}` : `blocked on ${deniedOn}`}
                    </span>
                  </>
                )}
              </span>
            </td>
            <td className={TD}>
              {actions.length > 0 && (
                <MoreActions
                  actions={actions}
                  triggerAriaLabel={`More for ${tool.name}`}
                />
              )}
            </td>
          </tr>
        );
      })}
    </>
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
  return `Allows ${LEVEL_LABEL[rule.level]} · ${parts.join(" · ")}`;
}

interface OtherRuleItem {
  key: string;
  text: string;
  onRemove: () => void;
}

function OtherMcpRules({ items }: { items: OtherRuleItem[] }): JSX.Element {
  return (
    <div className="border-border w-full border-t">
      <div className="border-border border-b px-4 py-3">
        <div className="text-eyebrow text-muted-foreground">Other rules</div>
        <Text muted small>
          Wider or narrower than a row above, or for a server that no longer
          exists. They stay as they are when you save.
        </Text>
      </div>
      <ul className="divide-border divide-y">
        {items.map(({ key, text, onRemove }) => {
          return (
            <li
              key={key}
              className="flex items-center justify-between gap-3 px-4 py-2"
            >
              <span className="text-sm">{text}</span>
              <Button
                variant="tertiary"
                size="sm"
                onClick={onRemove}
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
