import { Input } from "@/components/ui/Input";
import { ResizablePanel } from "@/components/ui/ResizablePanel";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { Lock, MousePointerClick, Search, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router";
import type { McpToolRun } from "./runMcpTool";
import { InspectSteps } from "./InspectState";
import { ToolRunPanel } from "./ToolRunPanel";
import {
  isDestructive,
  unavailableReasonLabel,
  type InspectTool,
} from "./types";
import { isFailing, type ToolHealth } from "./useToolHealth";

type PanelTab = "run" | "chat";

/**
 * The Inspect split view: what agents see on the left, a panel to run the
 * selected tool (or chat with the server) on the right. Server-specific pages
 * supply the tools, the sentence above them, and how a tool runs.
 */
export function InspectWorkbench({
  lens,
  tools,
  isLoading,
  state,
  notice,
  healthFor,
  runTool,
  runner,
  chat,
  accessHref,
}: {
  /** One sentence naming who and which mode the list is for. */
  lens: ReactNode;
  tools: InspectTool[];
  isLoading: boolean;
  /** Replaces the list when there is nothing to list yet (sign in, error). */
  state?: ReactNode;
  /** An action that sits above the list, e.g. tool metadata drift. */
  notice?: ReactNode;
  healthFor: (toolName: string) => ToolHealth | undefined;
  runTool?: (
    tool: InspectTool,
    args: Record<string, unknown>,
  ) => Promise<McpToolRun>;
  /** Replaces the per-tool form, e.g. Code Mode's program runner. */
  runner?: ReactNode;
  /** The chat tab's content; the tab is hidden without it. */
  chat?: ReactNode;
  /** Where blocked tools' rules can be changed. */
  accessHref?: string;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const [selectedName, setSelectedName] = useState<string>();
  const [tab, setTab] = useState<PanelTab>("run");
  const wide = useIsWide();
  const panelRef = useRef<HTMLElement>(null);

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
      {notice ? <div className="px-6 pb-4">{notice}</div> : null}
      {isLoading ? (
        <ToolListSkeleton />
      ) : state ? (
        state
      ) : (
        <ToolList
          sections={sections}
          total={tools.length}
          query={query}
          onQueryChange={setQuery}
          selectedName={selected?.name}
          onSelect={(name) => {
            setSelectedName(name);
            setTab("run");
            // Stacked, the panel sits below the list: bring it into view.
            if (!wide) {
              requestAnimationFrame(() =>
                panelRef.current?.scrollIntoView({ block: "nearest" }),
              );
            }
          }}
          healthFor={healthFor}
          accessHref={accessHref}
        />
      )}
    </>
  );

  const panel = (
    <aside
      ref={panelRef}
      aria-label="Selected tool"
      className={cn(
        "flex min-w-0 flex-col gap-5 px-6 py-5",
        wide
          ? "h-full min-h-0 overflow-y-auto"
          : "border-border min-h-[420px] border-t",
      )}
    >
      {(() => {
        const tabs = chat ? (
          <div role="tablist" className="flex gap-5 text-sm">
            {(["run", "chat"] as const).map((value) => (
              <button
                key={value}
                type="button"
                role="tab"
                aria-selected={tab === value}
                onClick={() => setTab(value)}
                className={cn(
                  "pb-1 capitalize",
                  tab === value
                    ? "text-foreground shadow-[inset_0_-1.5px_0_currentColor]"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {value}
              </button>
            ))}
          </div>
        ) : (
          <span />
        );
        // A state (error, sign-in) outranks a tool picked before it.
        if (tab !== "chat" && !state && !runner && selected) {
          // The run panel puts "Run as you" on the tab row.
          return (
            <ToolRunPanel
              key={selected.name}
              tool={selected}
              toolbar={tabs}
              health={healthFor(selected.name)}
              accessHref={accessHref}
              run={runTool ? (args) => runTool(selected, args) : undefined}
            />
          );
        }
        return (
          <>
            {chat ? tabs : null}
            {panelBody()}
          </>
        );
      })()}
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
            <div className="h-full min-h-0 min-w-0 overflow-y-auto py-4">
              {list}
            </div>
          </ResizablePanel.Pane>

          <ResizablePanel.ResizeHandle className="group bg-border hover:bg-foreground/40 data-[separator=active]:bg-foreground relative w-px transition-colors">
            {/* A wider grab area than the 1px rule it draws. */}
            <span className="absolute inset-y-0 -right-1.5 -left-1.5 cursor-col-resize" />
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
          <div className="min-w-0 py-4">{list}</div>
          {panel}
        </>
      )}
    </div>
  );

  function panelBody(): ReactNode {
    return tab === "chat" && chat ? (
      // The chat runs to the panel's edges, composer at the bottom.
      <div className="-mx-6 -mb-5 flex min-h-[420px] flex-1 flex-col">
        {chat}
      </div>
    ) : state ? (
      <InspectSteps />
    ) : runner ? (
      runner
    ) : (
      <PanelSummary
        tools={tools}
        healthFor={healthFor}
        onSelect={(name) => setSelectedName(name)}
      />
    );
  }
}

/**
 * The panel before a tool is chosen: what agents get at a glance, the tools
 * that need attention (one click opens them), and how to start.
 */
function PanelSummary({
  tools,
  healthFor,
  onSelect,
}: {
  tools: InspectTool[];
  healthFor: (toolName: string) => ToolHealth | undefined;
  onSelect: (name: string) => void;
}): JSX.Element {
  const usable = tools.filter((tool) => !tool.unavailable);
  const unavailable = tools.length - usable.length;
  const failing = failingTools(usable, healthFor);

  return (
    <div className="flex flex-col gap-6 pt-1">
      <div className="flex flex-col gap-3">
        <span className="text-eyebrow">At a glance</span>
        <dl className="border-border grid grid-cols-3 border">
          {[
            { label: "You can use", value: usable.length, tone: "" },
            {
              label: "Not available",
              value: unavailable,
              tone: unavailable > 0 ? "text-destructive" : "",
            },
            {
              label: "Failing",
              value: failing.length,
              tone: failing.length > 0 ? "text-destructive" : "",
            },
          ].map((stat) => (
            <div
              key={stat.label}
              className="border-border flex flex-col gap-1 border-l px-3 py-3 first:border-l-0"
            >
              <dt className="text-muted-foreground text-xs">{stat.label}</dt>
              <dd
                className={cn(
                  "font-display text-2xl leading-7 font-light tabular-nums",
                  stat.tone,
                )}
              >
                {stat.value}
              </dd>
            </div>
          ))}
        </dl>
      </div>

      {failing.length > 0 ? (
        <div className="flex flex-col gap-2">
          <span className="text-eyebrow">Needs attention</span>
          <ul className="border-border border-t">
            {failing.slice(0, 5).map(({ tool, health, sources }) => (
              <li key={tool.name}>
                <button
                  type="button"
                  onClick={() => onSelect(tool.name)}
                  className="border-border hover:bg-muted/60 flex w-full items-center justify-between gap-3 border-b py-2.5 text-left"
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
                  <span className="text-destructive shrink-0 text-[13px] tabular-nums">
                    {(health.failureRate * 100).toFixed(1)}% failed
                  </span>
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

function ToolList({
  sections,
  total,
  query,
  onQueryChange,
  selectedName,
  onSelect,
  healthFor,
  accessHref,
}: {
  sections: ToolSection[];
  total: number;
  query: string;
  onQueryChange: (query: string) => void;
  selectedName: string | undefined;
  onSelect: (name: string) => void;
  healthFor: (toolName: string) => ToolHealth | undefined;
  accessHref?: string;
}): JSX.Element {
  const [searching, setSearching] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

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
  }, [searching]);

  const closeSearch = () => {
    onQueryChange("");
    setSearching(false);
  };

  const firstHeading = sections[0]?.heading;
  // Searching an empty list is pointless.
  const canSearch = total > 0;
  const shown = sections.reduce((n, s) => n + s.tools.length, 0);

  return (
    <div className="flex flex-col">
      <div className="flex min-h-8 items-center justify-between gap-3 px-6 pb-2">
        <span className="text-eyebrow shrink-0">
          {searching && query
            ? `${shown} of ${total} tools`
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
                  onChange={onQueryChange}
                  placeholder={`Search ${total} tools`}
                  aria-label="Search tools"
                  icon="search"
                  tabIndex={searching ? undefined : -1}
                  className="h-8 w-full py-1 pr-8"
                  onKeyDown={(event) => {
                    if (event.key === "Escape") closeSearch();
                  }}
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
                  onClick={closeSearch}
                  className="text-muted-foreground hover:text-foreground absolute top-1/2 right-2 -translate-y-1/2"
                >
                  <X className="size-4" />
                </button>
              </div>
            </div>
            {searching ? null : (
              <button
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
        sections.map((section, index) => (
          <div key={section.key} className="flex flex-col">
            {index > 0 || searching ? (
              section.heading ? (
                <span className="text-eyebrow px-6 pt-7 pb-2">
                  {section.heading}
                </span>
              ) : null
            ) : null}
            <ul className="border-border border-t">
              {section.tools.map((tool) => (
                <ToolRow
                  key={tool.name}
                  tool={tool}
                  selected={tool.name === selectedName}
                  onSelect={() => onSelect(tool.name)}
                  health={healthFor(tool.name)}
                  accessHref={accessHref}
                />
              ))}
            </ul>
          </div>
        ))
      )}
    </div>
  );
}

function ToolRow({
  tool,
  selected,
  onSelect,
  health,
  accessHref,
}: {
  tool: InspectTool;
  selected: boolean;
  onSelect: () => void;
  health: ToolHealth | undefined;
  accessHref?: string;
}): JSX.Element {
  const failing = isFailing(health);
  return (
    <li
      className={cn(
        "border-border hover:bg-muted/60 flex items-center gap-4 border-b px-6 py-2.5 transition-colors",
        selected &&
          "bg-muted/50 shadow-[inset_2px_0_0_var(--color-foreground)]",
      )}
    >
      <button
        type="button"
        aria-pressed={selected}
        onClick={onSelect}
        className="flex min-w-0 flex-1 flex-col text-left"
      >
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
      </button>
      {tool.unavailable ? (
        accessHref ? (
          <Link
            to={accessHref}
            title="Change this in Team access"
            className="text-destructive flex shrink-0 items-center gap-1 text-xs underline-offset-2 hover:underline"
          >
            <Lock className="size-3" />
            {unavailableReasonLabel(tool.unavailable.reason)}
          </Link>
        ) : (
          <span className="text-destructive flex shrink-0 items-center gap-1 text-xs">
            <Lock className="size-3" />
            {unavailableReasonLabel(tool.unavailable.reason)}
          </span>
        )
      ) : failing && health ? (
        <span className="text-destructive shrink-0 font-sans text-[13px] tabular-nums">
          {(health.failureRate * 100).toFixed(1)}% failed
        </span>
      ) : null}
    </li>
  );
}

function ToolListSkeleton(): JSX.Element {
  return (
    <div className="flex flex-col">
      <div className="px-6 pb-2">
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

/**
 * An inline choice inside the lens sentence: the current value reads as part
 * of the sentence, and clicking it opens the options.
 */
export function LensChoice<T extends string>({
  label,
  value,
  options,
  onChange,
  footnote,
}: {
  /** Names the choice for screen readers, e.g. "Mode". */
  label: string;
  value: T;
  options: { value: T; label: string; hint?: string; disabled?: boolean }[];
  onChange: (value: T) => void;
  footnote?: ReactNode;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const current = options.find((option) => option.value === value);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        aria-label={`${label}: ${current?.label ?? value}`}
        className="text-foreground border-muted-foreground hover:border-foreground border-b border-dashed font-medium"
      >
        {current?.label ?? value}
      </PopoverTrigger>
      <PopoverContent align="start" className="flex w-72 flex-col gap-0.5 p-2">
        <span className="text-eyebrow px-2 pt-1 pb-2">{label}</span>
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            disabled={option.disabled}
            onClick={() => {
              onChange(option.value);
              setOpen(false);
            }}
            className={cn(
              "hover:bg-muted px-2 py-1.5 text-left text-sm disabled:cursor-not-allowed disabled:opacity-50",
              option.value === value && "bg-muted font-medium",
            )}
          >
            {option.label}
            {option.hint ? (
              <span className="text-muted-foreground font-normal">
                {" "}
                · {option.hint}
              </span>
            ) : null}
          </button>
        ))}
        {footnote ? (
          <Text muted className="border-border mt-1 border-t px-2 pt-2 text-xs">
            {footnote}
          </Text>
        ) : null}
      </PopoverContent>
    </Popover>
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
