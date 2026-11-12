import { Input } from "@/components/ui/Input";
import { ResizablePanel } from "@/components/ui/ResizablePanel";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { pluralize } from "@/lib/format";
import { scrollWithinPage } from "@/lib/scroll-within-page";
import { cn } from "@/lib/utils";
import { ArrowLeft, Lock, MousePointerClick, Search, X } from "lucide-react";
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
} from "react";
import type { McpToolRun } from "./runMcpTool";
import { InspectSteps } from "./InspectState";
import { FailedRate, ToolRunPanel } from "./ToolRunPanel";
import {
  EMPTY_DRAFT,
  isDestructive,
  unavailableReasonLabel,
  type InspectTool,
  type ToolDraft,
} from "./types";
import {
  FAILING_RATE,
  HEALTH_WINDOW_LABEL,
  isFailing,
  MIN_CALLS_FOR_RATE,
  type ToolHealth,
} from "./useToolHealth";

type PanelTab = "run" | "chat";

/** A stat that replaces "Not available" in the summary, e.g. for gateways. */
export interface UnavailableStat {
  label: string;
  value: number;
  isLoading?: boolean;
}

/**
 * The Inspect split view: what agents see on the left, a panel to run the
 * selected tool (or chat with the server) on the right. Server-specific pages
 * supply the tools, the sentence above them, and how a tool runs.
 */
export function InspectWorkbench({
  lens,
  tools,
  isLoading,
  isHealthLoading,
  state,
  notice,
  healthFor,
  runTool,
  runner,
  chat,
  accessHref,
  unavailableStat,
}: {
  /** One sentence naming who and which mode the list is for. */
  lens: ReactNode;
  tools: InspectTool[];
  isLoading: boolean;
  /** True while usage for the health figures is still loading. */
  isHealthLoading?: boolean;
  /** Replaces the list when there is nothing to list yet (sign in, error). */
  state?: ReactNode;
  /** An action that sits above the list, e.g. tool metadata drift. */
  notice?: ReactNode;
  healthFor: (toolName: string) => ToolHealth | undefined;
  runTool?: (
    tool: InspectTool,
    args: Record<string, unknown>,
  ) => Promise<McpToolRun>;
  /**
   * Replaces the per-tool form, e.g. Code Mode's program runner. It gets the
   * selected tool, if any, so it can offer to use it.
   */
  runner?: (selected: InspectTool | undefined) => ReactNode;
  /** The chat tab's content; the tab is hidden without it. */
  chat?: ReactNode;
  /** Where blocked tools' rules can be changed. */
  accessHref?: string;
  /** Overrides the summary's "Not available" figure. */
  unavailableStat?: UnavailableStat;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const [selectedName, setSelectedName] = useState<string>();
  const [tab, setTab] = useState<PanelTab>("run");
  // The chat mounts on first open and then stays, hidden, so switching back
  // and forth keeps the conversation.
  const [chatMounted, setChatMounted] = useState(false);
  // Arguments and the last result per tool, kept across tab and tool changes.
  const [drafts, setDrafts] = useState<Record<string, ToolDraft>>({});
  const wide = useIsWide();
  const panelRef = useRef<HTMLElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const tabsId = useId();

  const activeTab: PanelTab = chat ? tab : "run";
  const showTab = (value: PanelTab) => {
    setTab(value);
    if (value === "chat") setChatMounted(true);
  };

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return tools;
    return tools.filter(
      (tool) =>
        tool.name.toLowerCase().includes(needle) ||
        tool.description?.toLowerCase().includes(needle) ||
        tool.group?.toLowerCase().includes(needle),
    );
  }, [tools, query]);

  // Resolved during render, so a refetch that drops the tool clears it.
  const selected = tools.find((tool) => tool.name === selectedName);

  const sections = useMemo(() => groupTools(filtered), [filtered]);

  const list = (
    <>
      {notice ? <div className="px-6 pt-4">{notice}</div> : null}
      {isLoading ? (
        <ToolListSkeleton />
      ) : state ? (
        <div className="pt-4">{state}</div>
      ) : (
        <ToolList
          sections={sections}
          total={tools.length}
          query={query}
          onQueryChange={setQuery}
          selectedName={selected?.name}
          stickyHeader={wide}
          onSelect={(name) => {
            setSelectedName(name);
            showTab("run");
            // Stacked, the panel sits below the list: bring it into view.
            if (!wide) {
              requestAnimationFrame(() => {
                if (panelRef.current) scrollWithinPage(panelRef.current);
              });
            }
          }}
          healthFor={healthFor}
        />
      )}
    </>
  );

  const runBody =
    !state && !runner && selected ? (
      <ToolRunPanel
        key={selected.name}
        tool={selected}
        health={healthFor(selected.name)}
        isHealthLoading={isHealthLoading}
        accessHref={accessHref}
        run={runTool ? (args) => runTool(selected, args) : undefined}
        draft={drafts[selected.name] ?? EMPTY_DRAFT}
        onDraftChange={(update) =>
          setDrafts((current) => ({
            ...current,
            [selected.name]: update(current[selected.name] ?? EMPTY_DRAFT),
          }))
        }
      />
    ) : (
      <div className="flex flex-col gap-5 px-6 py-5">
        {state ? (
          <InspectSteps />
        ) : runner ? (
          runner(selected)
        ) : (
          <PanelSummary
            tools={tools}
            isLoading={isLoading}
            isHealthLoading={isHealthLoading}
            healthFor={healthFor}
            unavailableStat={unavailableStat}
            onSelect={(name) => setSelectedName(name)}
          />
        )}
      </div>
    );

  const panel = (
    <aside
      ref={panelRef}
      aria-label="Selected tool"
      className={cn(
        "flex min-w-0 flex-col",
        wide ? "h-full min-h-0" : "border-border border-t",
      )}
    >
      {!wide && selected ? (
        <button
          type="button"
          onClick={() => {
            if (listRef.current) scrollWithinPage(listRef.current);
            listRef.current
              ?.querySelector<HTMLElement>('[role="option"][tabindex="0"]')
              ?.focus({ preventScroll: true });
          }}
          className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 self-start px-6 pt-4 text-sm"
        >
          <ArrowLeft className="size-3.5" />
          All tools
        </button>
      ) : null}
      {chat ? (
        <div
          role="tablist"
          aria-label="Selected tool panel"
          className="border-border flex shrink-0 gap-5 border-b px-6 pt-4 text-sm"
        >
          {(
            [
              ["run", "Run"],
              ["chat", "Chat"],
            ] as const
          ).map(([value, label]) => (
            <button
              key={value}
              type="button"
              role="tab"
              id={`${tabsId}-${value}-tab`}
              aria-controls={`${tabsId}-${value}`}
              aria-selected={activeTab === value}
              onClick={() => showTab(value)}
              className={cn(
                "-mb-px pb-2",
                activeTab === value
                  ? "text-foreground shadow-[inset_0_-1.5px_0_currentColor]"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {label}
            </button>
          ))}
        </div>
      ) : null}
      <div
        role={chat ? "tabpanel" : undefined}
        id={`${tabsId}-run`}
        aria-labelledby={chat ? `${tabsId}-run-tab` : undefined}
        className={cn(
          activeTab !== "run" && "hidden",
          wide ? "min-h-0 flex-1 overflow-y-auto" : "min-h-[420px]",
        )}
      >
        {runBody}
      </div>
      {chat && chatMounted ? (
        // The chat fills the panel; its composer stays on the bottom edge.
        <div
          role="tabpanel"
          id={`${tabsId}-chat`}
          aria-labelledby={`${tabsId}-chat-tab`}
          className={cn(
            "flex min-w-0 flex-col",
            activeTab !== "chat" && "hidden",
            wide ? "min-h-0 flex-1" : "h-[min(640px,80vh)]",
          )}
        >
          {chat}
        </div>
      ) : null}
    </aside>
  );

  return (
    // Wide, it fills the tab's height: the list and the panel scroll inside
    // it, so the page itself never moves under the inspector. Narrower, the
    // panel stacks under the list and the page scrolls.
    <div className="border-border bg-card flex min-h-[480px] flex-1 flex-col border">
      <div className="border-border shrink-0 border-b px-6 py-4">{lens}</div>
      {wide ? (
        // Drag the rule between the list and the panel to resize them.
        <ResizablePanel useDefaultHandle={false} className="min-h-0 flex-1">
          <ResizablePanel.Pane
            minSize="35%"
            defaultSize="68%"
            className="min-w-0"
          >
            <div
              ref={listRef}
              className="h-full min-h-0 min-w-0 overflow-y-auto pb-4"
            >
              {list}
            </div>
          </ResizablePanel.Pane>

          <ResizablePanel.ResizeHandle className="group bg-border hover:bg-foreground/40 data-[separator=active]:bg-foreground relative z-10 w-px cursor-col-resize transition-colors">
            {/* An 11px grab area around the 1px rule it draws. */}
            <span className="absolute inset-y-0 -right-[5px] -left-[5px] cursor-col-resize" />
          </ResizablePanel.ResizeHandle>

          <ResizablePanel.Pane
            minSize="24%"
            defaultSize="32%"
            className="min-w-0"
          >
            {panel}
          </ResizablePanel.Pane>
        </ResizablePanel>
      ) : (
        <>
          <div ref={listRef} className="min-w-0 scroll-mt-4 pb-4">
            {list}
          </div>
          {panel}
        </>
      )}
    </div>
  );
}

/**
 * The panel before a tool is chosen: what agents get at a glance, the tools
 * that need attention (one click opens them), and how to start.
 */
function PanelSummary({
  tools,
  isLoading,
  isHealthLoading,
  healthFor,
  unavailableStat,
  onSelect,
}: {
  tools: InspectTool[];
  isLoading: boolean;
  isHealthLoading?: boolean;
  healthFor: (toolName: string) => ToolHealth | undefined;
  unavailableStat?: UnavailableStat;
  onSelect: (name: string) => void;
}): JSX.Element {
  const usable = tools.filter((tool) => !tool.unavailable);
  const unavailable = tools.length - usable.length;
  const failing = failingTools(usable, healthFor);
  const healthPending = isLoading || !!isHealthLoading;

  const stats = [
    {
      label: "You can use",
      value: usable.length,
      alert: false,
      pending: isLoading,
    },
    unavailableStat
      ? {
          label: unavailableStat.label,
          value: unavailableStat.value,
          alert: unavailableStat.value > 0,
          pending: isLoading || !!unavailableStat.isLoading,
        }
      : {
          label: "Not available",
          value: unavailable,
          alert: unavailable > 0,
          pending: isLoading,
        },
    {
      label: "Failing",
      value: failing.length,
      alert: failing.length > 0,
      pending: healthPending,
    },
  ];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-3">
        <span className="text-eyebrow">At a glance</span>
        <dl className="border-border grid grid-cols-3 border">
          {stats.map((stat) => (
            <div
              key={stat.label}
              className="border-border flex flex-col justify-between gap-1 border-l px-3 py-3 first:border-l-0"
            >
              <dt className="text-muted-foreground text-xs">{stat.label}</dt>
              <dd
                className={cn(
                  "font-display h-7 text-2xl leading-7 font-light tabular-nums",
                  stat.alert && !stat.pending && "text-destructive",
                )}
              >
                {stat.pending ? (
                  <Skeleton className="mt-1 h-5 w-8" />
                ) : (
                  stat.value
                )}
              </dd>
            </div>
          ))}
        </dl>
        <p className="text-muted-foreground text-xs leading-4">
          Failing means at least{" "}
          <span className="font-mono">{FAILING_RATE * 100}%</span> of{" "}
          <span className="font-mono">{MIN_CALLS_FOR_RATE}</span> or more calls
          failed in the {HEALTH_WINDOW_LABEL}.
        </p>
      </div>

      {!healthPending && failing.length > 0 ? (
        <div className="flex flex-col gap-2">
          <span className="text-eyebrow">
            Failing in the {HEALTH_WINDOW_LABEL}
          </span>
          <ul className="border-border border-t">
            {failing.slice(0, 5).map(({ tool, health, sources }) => (
              <li key={tool.name}>
                <button
                  type="button"
                  onClick={() => onSelect(tool.name)}
                  className="border-border hover:bg-muted/60 flex w-full items-center justify-between gap-3 border-b py-2.5 text-left font-sans"
                >
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate font-mono text-[13px]">
                      {tool.label ?? tool.name}
                    </span>
                    {sources.length > 0 ? (
                      <span className="text-muted-foreground truncate text-xs">
                        {sources.join(", ")}
                      </span>
                    ) : null}
                  </span>
                  <FailedRate
                    rate={health.failureRate}
                    className="text-destructive shrink-0 text-[13px]"
                  />
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <p className="text-muted-foreground flex items-start gap-2 text-sm leading-5">
        <MousePointerClick className="mt-0.5 size-4 shrink-0" />
        <span>
          Pick a tool to see its arguments and run it as you. Press{" "}
          <kbd className="border-border bg-muted border px-1 font-mono text-[11px]">
            /
          </kbd>{" "}
          to search.
        </span>
      </p>
    </div>
  );
}

/**
 * Usable tools whose calls fail often, worst first. Usage is recorded by the
 * name an agent called, so tools from different gateway servers can share one
 * usage record (two servers' `get_metrics`): that record lists once, naming
 * every server it may cover.
 */
function failingTools(
  tools: InspectTool[],
  healthFor: (toolName: string) => ToolHealth | undefined,
): { tool: InspectTool; health: ToolHealth; sources: string[] }[] {
  const byHealth = new Map<
    ToolHealth,
    { tool: InspectTool; health: ToolHealth; sources: string[] }
  >();
  for (const tool of tools) {
    const health = healthFor(tool.name);
    if (!health || !isFailing(health)) continue;
    const entry = byHealth.get(health);
    if (entry) {
      if (tool.source && !entry.sources.includes(tool.source))
        entry.sources.push(tool.source);
      continue;
    }
    byHealth.set(health, {
      tool,
      health,
      sources: tool.source ? [tool.source] : [],
    });
  }
  return [...byHealth.values()].sort(
    (a, b) => b.health.failureRate - a.health.failureRate,
  );
}

interface ToolSection {
  key: string;
  heading: string | undefined;
  tools: InspectTool[];
}

/**
 * Usable tools stay in their groups, in listing order. Tools this viewer
 * can't use collect in one section at the end, whatever group they came from.
 */
function groupTools(tools: InspectTool[]): ToolSection[] {
  const sections: ToolSection[] = [];
  const byGroup = new Map<string, ToolSection>();
  const unavailable: InspectTool[] = [];
  for (const tool of tools) {
    if (tool.unavailable) {
      unavailable.push(tool);
      continue;
    }
    const key = tool.group ?? "";
    let section = byGroup.get(key);
    if (!section) {
      section = { key: `group:${key}`, heading: tool.group, tools: [] };
      byGroup.set(key, section);
      sections.push(section);
    }
    section.tools.push(tool);
  }
  if (unavailable.length > 0) {
    sections.push({
      key: "unavailable",
      heading: `Not available to you · ${unavailable.length}`,
      tools: unavailable,
    });
  }
  return sections;
}

/**
 * The tool list is one listbox. From the search field, ArrowUp and ArrowDown
 * move an active row (focus stays in the field) and Enter picks it. On the
 * rows themselves, the arrows move focus and Enter or Space picks.
 */
function ToolList({
  sections,
  total,
  query,
  onQueryChange,
  selectedName,
  stickyHeader,
  onSelect,
  healthFor,
}: {
  sections: ToolSection[];
  total: number;
  query: string;
  onQueryChange: (query: string) => void;
  selectedName: string | undefined;
  /** Pins the heading and search to the top of the list's scroller. */
  stickyHeader: boolean;
  onSelect: (name: string) => void;
  healthFor: (toolName: string) => ToolHealth | undefined;
}): JSX.Element {
  const [searching, setSearching] = useState(false);
  const [activeName, setActiveName] = useState<string>();
  const inputRef = useRef<HTMLInputElement>(null);
  const searchButtonRef = useRef<HTMLButtonElement>(null);
  // Set when search closes from the keyboard, so focus lands on the icon.
  const restoreFocus = useRef(false);
  const listId = useId();

  const order = useMemo(() => sections.flatMap((s) => s.tools), [sections]);
  const optionId = (index: number) => `${listId}-option-${index}`;
  const indexOf = (name: string | undefined) =>
    name === undefined ? -1 : order.findIndex((tool) => tool.name === name);

  // The first match is active while a query is typed, until the arrows move.
  const active =
    activeName !== undefined && indexOf(activeName) >= 0
      ? activeName
      : searching && query
        ? order[0]?.name
        : undefined;
  const activeIndex = indexOf(active);
  // One row is in the tab order: the selected one, else the first.
  const tabbable = indexOf(selectedName) >= 0 ? selectedName : order[0]?.name;

  // `/` opens search from anywhere on the page, unless the user is typing.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "/" || event.metaKey || event.ctrlKey) return;
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.isContentEditable ||
          ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))
      )
        return;
      if (total === 0) return;
      event.preventDefault();
      setSearching(true);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [total]);
  useEffect(() => {
    if (searching) inputRef.current?.focus();
    else if (restoreFocus.current) {
      restoreFocus.current = false;
      searchButtonRef.current?.focus();
    }
  }, [searching]);

  const changeQuery = (next: string) => {
    setActiveName(undefined);
    onQueryChange(next);
  };
  const closeSearch = (returnFocus: boolean) => {
    restoreFocus.current = returnFocus;
    changeQuery("");
    setSearching(false);
  };

  const activate = (index: number) => {
    const tool = order[index];
    if (!tool) return;
    setActiveName(tool.name);
    document
      .getElementById(optionId(index))
      ?.scrollIntoView?.({ block: "nearest" });
  };
  const onSearchKeyDown = (event: ReactKeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      closeSearch(true);
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      activate(Math.min(activeIndex + 1, order.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      activate(Math.max(activeIndex - 1, 0));
    } else if (event.key === "Enter" && active) {
      event.preventDefault();
      onSelect(active);
    }
  };
  const onRowKeyDown = (
    event: ReactKeyboardEvent<HTMLDivElement>,
    index: number,
  ) => {
    const focusRow = (next: number) => {
      const row = document.getElementById(optionId(next));
      if (!row) return;
      row.focus();
      row.scrollIntoView?.({ block: "nearest" });
    };
    if (event.key === "ArrowDown") {
      event.preventDefault();
      focusRow(Math.min(index + 1, order.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      focusRow(Math.max(index - 1, 0));
    } else if (event.key === "Home") {
      event.preventDefault();
      focusRow(0);
    } else if (event.key === "End") {
      event.preventDefault();
      focusRow(order.length - 1);
    } else if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      const tool = order[index];
      if (tool) onSelect(tool.name);
    }
  };

  const firstHeading = sections[0]?.heading;
  // Searching an empty list is pointless.
  const canSearch = total > 0;
  const shown = order.length;
  let rowIndex = 0;

  return (
    <div className="flex flex-col">
      <div
        className={cn(
          "bg-card flex min-h-8 items-center justify-between gap-3 px-6 pt-4 pb-2",
          stickyHeader && "sticky top-0 z-10",
        )}
      >
        <span className="text-eyebrow shrink-0">
          {searching && query
            ? `${shown} of ${pluralize(total, "tool")}`
            : searching
              ? "Tools"
              : (firstHeading ?? "Tools")}
        </span>
        {canSearch ? (
          // The field grows leftward out of the icon, on the same row.
          <div className="relative flex min-w-0 flex-1 items-center justify-end">
            <div
              aria-hidden={!searching}
              className={cn(
                "flex min-w-0 items-center overflow-hidden transition-[width,opacity,visibility] duration-200 ease-out motion-reduce:transition-none",
                searching
                  ? "visible w-full max-w-72 opacity-100"
                  : "invisible w-0 opacity-0",
              )}
            >
              <div className="relative w-full">
                <Input
                  ref={inputRef}
                  value={query}
                  onChange={changeQuery}
                  placeholder={`Search ${pluralize(total, "tool")}`}
                  aria-label="Search tools"
                  role="combobox"
                  aria-expanded={searching}
                  aria-controls={listId}
                  aria-autocomplete="list"
                  aria-activedescendant={
                    activeIndex >= 0 ? optionId(activeIndex) : undefined
                  }
                  icon="search"
                  tabIndex={searching ? undefined : -1}
                  className="h-8 w-full py-1 pr-8"
                  onKeyDown={onSearchKeyDown}
                  onBlur={() => {
                    if (!query) setSearching(false);
                  }}
                />
                <button
                  type="button"
                  aria-label="Close search"
                  tabIndex={searching ? undefined : -1}
                  // Keeps focus in the field, so blur doesn't collapse first.
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => closeSearch(true)}
                  className="text-muted-foreground hover:text-foreground absolute top-1/2 right-2 -translate-y-1/2"
                >
                  <X className="size-4" />
                </button>
              </div>
            </div>
            {searching ? null : (
              <button
                ref={searchButtonRef}
                type="button"
                aria-label="Search tools"
                title="Search tools (/)"
                onClick={() => setSearching(true)}
                className="text-muted-foreground hover:text-foreground absolute right-0"
              >
                <Search className="size-4" />
              </button>
            )}
          </div>
        ) : null}
      </div>

      {shown === 0 ? (
        <Text muted small className="px-6 py-6">
          {query ? `No tools match “${query}”.` : "This server lists no tools."}
        </Text>
      ) : (
        <div
          role="listbox"
          id={listId}
          aria-label="Tools"
          className="flex flex-col"
        >
          {sections.map((section, index) => {
            const headingId = `${listId}-group-${index}`;
            const showHeading = (index > 0 || searching) && !!section.heading;
            return (
              <div
                key={section.key}
                role="group"
                aria-labelledby={showHeading ? headingId : undefined}
                aria-label={
                  showHeading ? undefined : (section.heading ?? "Tools")
                }
                className="flex flex-col"
              >
                {showHeading ? (
                  <span id={headingId} className="text-eyebrow px-6 pt-7 pb-2">
                    {section.heading}
                  </span>
                ) : null}
                <div className="border-border border-t">
                  {section.tools.map((tool) => {
                    const at = rowIndex++;
                    return (
                      <ToolRow
                        key={tool.name}
                        id={optionId(at)}
                        tool={tool}
                        selected={tool.name === selectedName}
                        active={tool.name === active}
                        tabbable={tool.name === tabbable}
                        onSelect={() => onSelect(tool.name)}
                        onKeyDown={(event) => onRowKeyDown(event, at)}
                        health={healthFor(tool.name)}
                      />
                    );
                  })}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function ToolRow({
  id,
  tool,
  selected,
  active,
  tabbable,
  onSelect,
  onKeyDown,
  health,
}: {
  id: string;
  tool: InspectTool;
  selected: boolean;
  /** Highlighted from the search field's arrow keys. */
  active: boolean;
  tabbable: boolean;
  onSelect: () => void;
  onKeyDown: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
  health: ToolHealth | undefined;
}): JSX.Element {
  const failing = isFailing(health);
  return (
    <div
      id={id}
      role="option"
      aria-selected={selected}
      tabIndex={tabbable ? 0 : -1}
      onClick={onSelect}
      onKeyDown={onKeyDown}
      className={cn(
        "border-border hover:bg-muted/60 focus-visible:ring-foreground/40 flex cursor-pointer items-center gap-4 border-b px-6 py-2.5 transition-colors outline-none focus-visible:ring-1 focus-visible:ring-inset",
        selected &&
          "bg-muted/50 shadow-[inset_2px_0_0_var(--color-foreground)]",
        active && "bg-muted/60 ring-foreground/40 ring-1 ring-inset",
      )}
    >
      <span className="flex min-w-0 flex-1 flex-col text-left">
        <span className="flex min-w-0 items-center gap-2">
          <span
            className={cn(
              "truncate font-mono text-[13px]",
              tool.unavailable && "text-muted-foreground",
            )}
          >
            {tool.label ?? tool.name}
          </span>
          {isDestructive(tool) ? (
            <span className="text-muted-foreground shrink-0 font-mono text-[10px] leading-4 tracking-wide uppercase">
              Destructive
            </span>
          ) : null}
        </span>
        <span className="text-muted-foreground truncate text-[13px] leading-5">
          {tool.description || "No description"}
        </span>
      </span>
      {tool.unavailable ? (
        // The panel links to the rule once the row is picked.
        <span className="text-destructive flex shrink-0 items-center gap-1 text-xs">
          <Lock className="size-3" />
          {unavailableReasonLabel(tool.unavailable.reason)}
        </span>
      ) : failing && health ? (
        <FailedRate
          rate={health.failureRate}
          className="text-destructive shrink-0 text-[13px]"
        />
      ) : null}
    </div>
  );
}

function ToolListSkeleton(): JSX.Element {
  return (
    <div className="flex flex-col">
      <div className="px-6 pt-4 pb-2">
        <Skeleton className="h-3 w-24" />
      </div>
      <div className="border-border border-t">
        {Array.from({ length: 6 }).map((_, index) => (
          <div
            key={index}
            className="border-border flex flex-col gap-2 border-b px-6 py-3"
          >
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-3 w-72 max-w-full" />
          </div>
        ))}
      </div>
    </div>
  );
}

/** Tailwind's `xl` breakpoint: below it the split is too cramped to read. */
const WIDE_QUERY = "(min-width: 80rem)";

function useIsWide(): boolean {
  const [wide, setWide] = useState(
    () =>
      typeof window.matchMedia !== "function" ||
      window.matchMedia(WIDE_QUERY).matches,
  );
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const query = window.matchMedia(WIDE_QUERY);
    const onChange = () => setWide(query.matches);
    onChange();
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);
  return wide;
}
