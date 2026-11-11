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

  return (
    // Fills the tab's height: the list and the panel scroll inside it, so
    // the page itself never moves under the inspector.
    <div className="border-border bg-card flex min-h-[480px] flex-1 flex-col border">
      <div className="border-border shrink-0 border-b px-6 py-4">{lens}</div>
      {/* Drag the rule between the list and the panel to resize them. */}
      <ResizablePanel useDefaultHandle={false} className="min-h-0 flex-1">
        <ResizablePanel.Pane
          minSize="35%"
          defaultSize="68%"
          className="min-w-0"
        >
          <div className="h-full min-h-0 min-w-0 overflow-y-auto py-4">
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
                }}
                healthFor={healthFor}
                accessHref={accessHref}
              />
            )}
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
          <aside
            aria-label="Selected tool"
            className="flex h-full min-h-0 min-w-0 flex-col gap-5 overflow-y-auto px-6 py-5"
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
              if (tab !== "chat" && !runner && selected) {
                // The run panel puts "Run as you" on the tab row.
                return (
                  <ToolRunPanel
                    key={selected.name}
                    tool={selected}
                    toolbar={tabs}
                    health={healthFor(selected.name)}
                    run={
                      runTool ? (args) => runTool(selected, args) : undefined
                    }
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
        </ResizablePanel.Pane>
      </ResizablePanel>
    </div>
  );

  function panelBody(): ReactNode {
    return tab === "chat" && chat ? (
      // The chat runs to the panel's edges, composer at the bottom.
      <div className="-mx-6 -mb-5 flex min-h-[420px] flex-1 flex-col">
        {chat}
      </div>
    ) : runner ? (
      runner
    ) : state ? (
      <InspectSteps />
    ) : (
      <PanelHint
        heading="Select a tool"
        description="Pick a tool from the list to see its arguments, run it as you, and read the response."
      />
    );
  }
}

/** The quiet panel state before there is a tool to show. */
function PanelHint({
  heading,
  description,
}: {
  heading: string;
  description: string;
}): JSX.Element {
  return (
    <div className="flex flex-col items-start gap-2 pt-6">
      <MousePointerClick className="text-muted-foreground size-5" />
      <p className="text-foreground text-sm font-medium">{heading}</p>
      <p className="text-muted-foreground max-w-[30ch] text-sm">
        {description}
      </p>
    </div>
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
      event.preventDefault();
      setSearching(true);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  useEffect(() => {
    if (searching) inputRef.current?.focus();
  }, [searching]);

  const closeSearch = () => {
    onQueryChange("");
    setSearching(false);
  };

  const firstHeading = sections[0]?.heading;
  const shown = sections.reduce((n, s) => n + s.tools.length, 0);

  return (
    <div className="flex flex-col">
      <div className="flex min-h-8 items-center justify-between gap-3 px-6 pb-2">
        {searching ? (
          <div className="flex flex-1 items-center gap-2">
            <Input
              ref={inputRef}
              value={query}
              onChange={onQueryChange}
              placeholder={`Search ${total} tools`}
              aria-label="Search tools"
              icon="search"
              className="flex-1"
              onKeyDown={(event) => {
                if (event.key === "Escape") closeSearch();
              }}
            />
            <button
              type="button"
              aria-label="Close search"
              onClick={closeSearch}
              className="text-muted-foreground hover:text-foreground"
            >
              <X className="size-4" />
            </button>
          </div>
        ) : (
          <>
            <span className="text-eyebrow">{firstHeading ?? "Tools"}</span>
            <button
              type="button"
              aria-label="Search tools"
              title="Search tools (/)"
              onClick={() => setSearching(true)}
              className="text-muted-foreground hover:text-foreground"
            >
              <Search className="size-4" />
            </button>
          </>
        )}
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
