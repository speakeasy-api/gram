import * as React from "react";
import { useState, useEffect } from "react";
import { cva } from "class-variance-authority";
import {
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  ChevronUpIcon,
  CopyIcon,
  EyeIcon,
  EyeOffIcon,
  LoaderIcon,
  SearchIcon,
  TriangleAlertIcon,
  XIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { codeToHtml, BundledLanguage } from "shiki";
import { Button } from "./button";
import { Popover, PopoverAnchor, PopoverContent } from "./popover";

/* -----------------------------------------------------------------------------
 * Status indicator styles
 * -------------------------------------------------------------------------- */

const statusVariants = cva(
  "flex size-5 items-center justify-center rounded-full",
  {
    variants: {
      status: {
        pending: "border border-dashed border-muted-foreground/50",
        running: "text-primary",
        complete: "text-green-600 dark:text-green-500",
        error: "text-destructive",
        approval: "text-amber-500",
      },
    },
    defaultVariants: {
      status: "pending",
    },
  },
);

/* -----------------------------------------------------------------------------
 * Types
 * -------------------------------------------------------------------------- */

type ToolStatus = "pending" | "running" | "complete" | "error" | "approval";

type ContentItem =
  | { type: "text"; text: string; _meta?: { "getgram.ai/mime-type"?: string } }
  | {
      type: "image";
      data: string;
      _meta?: { "getgram.ai/mime-type"?: string };
    };

/** MCP tool annotations providing hints about tool behavior */
interface ToolAnnotations {
  /** Human-readable display name for the tool */
  title?: string;
  /** If true, the tool does not modify its environment */
  readOnlyHint?: boolean;
  /** If true, the tool may perform destructive updates */
  destructiveHint?: boolean;
  /** If true, repeated calls with same args have no additional effect */
  idempotentHint?: boolean;
  /** If true, tool interacts with external entities */
  openWorldHint?: boolean;
}

/** Marks a tool section (arguments/output) as containing flagged substrings,
 * so the section header shows a warning and the expanded body lets you jump
 * between matches. */
/** One flagged finding within a tool section. */
interface SectionMatch {
  /** Literal substring to highlight and step to. */
  value: string;
  /** Short rule label shown when this match is active (e.g. "pii.phone_number"). */
  label?: string;
  /** Optional action for this finding, surfaced as a button while it is the
   * active match (e.g. open the create-exclusion flow). */
  onExclude?: () => void;
}

interface SectionHighlight {
  /** Findings to highlight and step through with the next/prev controls. */
  matches: SectionMatch[];
  /** Dot out the matched characters until the viewer reveals them (secrets). */
  masked?: boolean;
  /** Optional host-supplied badge rendered in the section header (e.g. a risk
   * pill). Replaces the default warning icon when present. */
  headerBadge?: React.ReactNode;
  /** Mark colour: "risk" (red, default) for findings, "search" (yellow) for a
   * text-search hit. */
  tone?: "risk" | "search";
}

interface ToolUIProps {
  /** Display name of the tool */
  name: string;
  /** Optional icon to display (defaults to first letter of name) */
  icon?: React.ReactNode;
  /** Provider/source name (e.g., "Notion", "GitHub") */
  provider?: string;
  /** Current status of the tool execution */
  status?: ToolStatus;
  /** Request/input data - can be string or object */
  request?: string | Record<string, unknown>;
  /** Result/output data - can be string, object, or structured content array */
  result?: string | Record<string, unknown> | { content: ContentItem[] };
  /** Whether the tool card starts expanded */
  defaultExpanded?: boolean;
  /** Flag matches inside the arguments (risk review). */
  requestHighlight?: SectionHighlight;
  /** Flag matches inside the output (risk review). */
  resultHighlight?: SectionHighlight;
  /** When set, highlight occurrences of this query (case-insensitive) in the
   * tool name — e.g. a thread search for "customer" lights up `get_customer`. */
  nameQuery?: string;
  /** Whether this tool holds the active thread-search match: bright highlights
   * (name + sections) when true, pale when false. */
  searchActive?: boolean;
  /** Additional class names */
  className?: string;
  /** MCP tool annotations */
  annotations?: ToolAnnotations;
  /** Approval callbacks */
  onApproveOnce?: () => void;
  onApproveForSession?: () => void;
  onDeny?: () => void;
}

interface ToolUISectionProps {
  /** Section title */
  title: string;
  /** Content to display - string or object (will be JSON stringified) */
  content: string | Record<string, unknown> | { content: ContentItem[] };
  /** Whether section starts expanded */
  defaultExpanded?: boolean;
  /** Enable syntax highlighting */
  highlightSyntax?: boolean;
  /** Language hint for syntax highlighting */
  language?: BundledLanguage;
  /** Flagged substrings — renders a navigable highlighted view + header icon. */
  highlight?: SectionHighlight;
  /** Search tone only: whether this tool holds the active thread match (bright
   * vs pale marks). */
  searchActive?: boolean;
}

/* -----------------------------------------------------------------------------
 * Helper Functions
 * -------------------------------------------------------------------------- */

function getLanguageFromMimeType(
  mimeType: string,
): BundledLanguage | undefined {
  switch (mimeType) {
    case "text/markdown":
      return "markdown";
    case "text/html":
      return "html";
    case "text/css":
      return "css";
    case "application/json":
      return "json";
    case "text/javascript":
      return "javascript";
    case "text/typescript":
      return "typescript";
    case "text/python":
      return "python";
    default:
      return undefined;
  }
}

function formatTextForLanguage(
  text: string,
  language: BundledLanguage | undefined,
): string {
  if (language === "json") {
    try {
      return JSON.stringify(JSON.parse(text), null, 2);
    } catch {
      return text;
    }
  }
  return text;
}

function isStructuredContent(
  content: unknown,
): content is { content: ContentItem[] } {
  return (
    typeof content === "object" &&
    content !== null &&
    "content" in content &&
    Array.isArray((content as { content: unknown }).content)
  );
}

/* -----------------------------------------------------------------------------
 * Helper Components
 * -------------------------------------------------------------------------- */

function StatusIndicator({
  status,
}: {
  status: ToolStatus;
}): React.JSX.Element {
  return (
    <div className={cn(statusVariants({ status }))}>
      {status === "pending" && null}
      {status === "running" && <LoaderIcon className="size-4 animate-spin" />}
      {status === "complete" && <CheckIcon className="size-4" />}
      {status === "error" && <XIcon className="size-4" />}
      {status === "approval" && (
        <LoaderIcon className="size-4 animate-spin text-muted-foreground" />
      )}
    </div>
  );
}

function CopyButton({ content }: { content: string }): React.JSX.Element {
  const [copied, setCopied] = useState(false);

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    await navigator.clipboard.writeText(content);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <button
      onClick={(e) => {
        void handleCopy(e);
      }}
      className="rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
      aria-label="Copy to clipboard"
    >
      {copied ? (
        <CheckIcon className="size-4" />
      ) : (
        <CopyIcon className="size-4" />
      )}
    </button>
  );
}

/* -----------------------------------------------------------------------------
 * SyntaxHighlightedCode - Code block with shiki syntax highlighting
 * -------------------------------------------------------------------------- */

/** Max characters to send through shiki — above this we skip highlighting. */
const SHIKI_CHAR_LIMIT = 8_000;
/** Max lines shown in the collapsed preview. */
const PREVIEW_LINE_LIMIT = 50;

function truncateToLines(text: string, maxLines: number) {
  let pos = 0;
  for (let i = 0; i < maxLines; i++) {
    const next = text.indexOf("\n", pos);
    if (next === -1) return { text, truncated: false, totalLines: i + 1 };
    pos = next + 1;
  }
  const totalLines = text.split("\n").length;
  return { text: text.slice(0, pos), truncated: true, totalLines };
}

function SyntaxHighlightedCode({
  text,
  language,
  className,
}: {
  text: string;
  language?: BundledLanguage;
  className?: string;
}): React.JSX.Element {
  const [highlightedCode, setHighlightedCode] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);

  const preview = React.useMemo(
    () => truncateToLines(text, PREVIEW_LINE_LIMIT),
    [text],
  );
  const displayText = expanded ? text : preview.text;
  const canHighlight = displayText.length <= SHIKI_CHAR_LIMIT;

  useEffect(() => {
    setHighlightedCode(null);
    if (!language || !canHighlight) return;
    let cancelled = false;
    void codeToHtml(displayText, {
      lang: language,
      theme: "github-dark-default",
      rootStyle: "background-color: transparent;",
      transformers: [
        {
          pre(node) {
            node.properties.class =
              "w-full py-3 px-4 max-h-[300px] overflow-y-auto whitespace-pre-wrap break-all text-left text-sm";
          },
        },
      ],
    }).then((html) => {
      if (!cancelled) setHighlightedCode(html);
    });
    return () => {
      cancelled = true;
    };
  }, [displayText, language, canHighlight]);

  const showMoreButton = preview.truncated && !expanded && (
    <button
      type="button"
      onClick={() => setExpanded(true)}
      className="w-full bg-slate-800/90 px-4 py-2 text-left text-xs text-slate-400 transition-colors hover:text-slate-200"
    >
      Show all {preview.totalLines} lines…
    </button>
  );

  if (!canHighlight || !highlightedCode) {
    return (
      <div className={cn("w-full", className)}>
        <pre className="max-h-[300px] w-full overflow-y-auto bg-slate-800/90 px-4 py-3 text-sm break-all whitespace-pre-wrap text-slate-100">
          {displayText}
        </pre>
        {showMoreButton}
      </div>
    );
  }

  return (
    <div className={cn("w-full", className)}>
      <div
        className="w-full bg-slate-800/90"
        dangerouslySetInnerHTML={{ __html: highlightedCode }}
      />
      {showMoreButton}
    </div>
  );
}

/* -----------------------------------------------------------------------------
 * HighlightedCode - plain code view with flagged matches you can step through
 * -------------------------------------------------------------------------- */

interface MatchHit {
  start: number;
  end: number;
  /** Index into the `matches` array that produced this hit. */
  matchIndex: number;
}

function findMatchHits(
  text: string,
  values: string[],
  caseInsensitive = false,
): MatchHit[] {
  // Risk findings match an exact value; a text-search hit matches case-
  // insensitively (the server search is ILIKE). Tool content is monospace
  // code/JSON, so lowercasing doesn't shift offsets in practice.
  const haystack = caseInsensitive ? text.toLowerCase() : text;
  const hits: MatchHit[] = [];
  values.forEach((value, matchIndex) => {
    if (!value) return;
    const needle = caseInsensitive ? value.toLowerCase() : value;
    let from = 0;
    let idx = haystack.indexOf(needle, from);
    while (idx !== -1) {
      hits.push({ start: idx, end: idx + value.length, matchIndex });
      from = idx + value.length;
      idx = haystack.indexOf(needle, from);
    }
  });
  hits.sort((a, b) => a.start - b.start);
  // Coalesce overlapping ranges so the renderer's sequential, non-overlapping
  // slice walk stays correct. A merged range keeps the first hit's matchIndex
  // (overlapping distinct findings are rare; correct rendering wins).
  const merged: MatchHit[] = [];
  for (const hit of hits) {
    const last = merged[merged.length - 1];
    if (last && hit.start <= last.end) last.end = Math.max(last.end, hit.end);
    else merged.push({ ...hit });
  }
  return merged;
}

function maskMatch(value: string): string {
  // Mask character-for-character so toggling reveal doesn't change the length
  // (the tool view is monospace, so equal length means zero layout shift).
  return "•".repeat(value.length);
}

function HighlightedCode({
  text,
  matches,
  masked,
  tone = "risk",
  searchActive = false,
}: {
  text: string;
  matches: SectionMatch[];
  masked?: boolean;
  tone?: "risk" | "search";
  /** Search tone only: whether this tool holds the active thread match. Active
   * → bright marks; inactive → pale. (Risk tone steps per-section instead.) */
  searchActive?: boolean;
}): React.JSX.Element {
  const hits = React.useMemo(
    () =>
      findMatchHits(
        text,
        matches.map((m) => m.value),
        tone === "search",
      ),
    [text, matches, tone],
  );
  const count = hits.length;
  const [active, setActive] = useState(0);
  const [revealed, setRevealed] = useState(!masked);
  const markRefs = React.useRef<Array<HTMLElement | null>>([]);
  const preRef = React.useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (active >= count && count > 0) setActive(0);
  }, [count, active]);
  // Center the active match within the code block *only* — adjust the <pre>'s
  // own scrollTop rather than scrollIntoView(), which would also yank the
  // surrounding sheet. Runs on mount (focus the first match) and on each step.
  useEffect(() => {
    const pre = preRef.current;
    const mark = markRefs.current[active];
    if (!pre || !mark) return;
    const markRect = mark.getBoundingClientRect();
    const preRect = pre.getBoundingClientRect();
    pre.scrollTop +=
      markRect.top - preRect.top - pre.clientHeight / 2 + markRect.height / 2;
  }, [active, count]);

  const go = (delta: number) => {
    if (count === 0) return;
    setActive((a) => (a + delta + count) % count);
  };

  const activeMatch = hits[active]
    ? matches[hits[active]!.matchIndex]
    : undefined;

  const segments: React.ReactNode[] = [];
  let pos = 0;
  hits.forEach((hit, i) => {
    if (hit.start > pos)
      segments.push(<span key={`t${i}`}>{text.slice(pos, hit.start)}</span>);
    const value = text.slice(hit.start, hit.end);
    segments.push(
      <mark
        key={`m${i}`}
        ref={(el) => {
          markRefs.current[i] = el;
        }}
        className={cn(
          // Fixed-width mono chip, lightened for the dark code surface. The active
          // (currently navigated) match pops so prev/next navigation + auto-scroll
          // have a visible target; the rest stay a darker shade. Risk findings are
          // red; a plain text-search hit is yellow.
          "rounded-sm px-0.5 font-mono ring-1",
          tone === "search"
            ? // Search nav is per-row, so all occurrences here share the row's
              // active state: bright when this tool is the active match, else pale.
              searchActive
              ? "bg-yellow-400 text-yellow-950 ring-yellow-300"
              : "bg-yellow-800/50 text-yellow-200/90 ring-yellow-700/50"
            : i === active
              ? "bg-red-700 text-red-50 ring-red-400"
              : "bg-red-900 text-red-300 ring-red-800",
        )}
      >
        {masked && !revealed ? maskMatch(value) : value}
      </mark>,
    );
    pos = hit.end;
  });
  if (pos < text.length)
    segments.push(<span key="tail">{text.slice(pos)}</span>);

  return (
    <div className="w-full">
      {count > 0 && (
        <div className="flex items-center justify-between gap-3 bg-slate-900 px-4 py-2 text-xs text-slate-300">
          <div className="flex min-w-0 items-center gap-2">
            {tone === "search" ? (
              <span className="flex shrink-0 items-center gap-1.5 font-medium text-yellow-300">
                <SearchIcon className="size-3.5" />
                {count} {count === 1 ? "match" : "matches"}
              </span>
            ) : (
              <span className="flex shrink-0 items-center gap-1.5 font-medium text-amber-400">
                <TriangleAlertIcon className="size-3.5" />
                {count} flagged {count === 1 ? "match" : "matches"}
              </span>
            )}
            {activeMatch?.label && (
              <span className="truncate rounded bg-slate-700/60 px-1.5 py-0.5 font-mono text-slate-300">
                {activeMatch.label}
              </span>
            )}
            {activeMatch?.onExclude && (
              <button
                type="button"
                onClick={activeMatch.onExclude}
                title="Create an exclusion for this finding"
                className="shrink-0 rounded px-1.5 py-0.5 text-slate-300 transition-colors hover:bg-slate-700 hover:text-white"
              >
                Create exclusion
              </button>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-3 text-slate-400">
            {masked && (
              <button
                type="button"
                onClick={() => setRevealed((r) => !r)}
                className="inline-flex items-center gap-1 transition-colors hover:text-slate-100"
              >
                {revealed ? (
                  <EyeIcon className="size-3.5" />
                ) : (
                  <EyeOffIcon className="size-3.5" />
                )}
                {revealed ? "Hide" : "Reveal"}
              </button>
            )}
            {count >= 1 && (
              <div className="flex items-center gap-0.5">
                <button
                  type="button"
                  onClick={() => go(-1)}
                  disabled={count <= 1}
                  className="rounded p-1 transition-colors hover:bg-slate-700 hover:text-slate-100 disabled:opacity-40 disabled:hover:bg-transparent"
                  aria-label="Previous match"
                >
                  <ChevronUpIcon className="size-3.5" />
                </button>
                <span className="text-slate-300 tabular-nums">
                  {active + 1}/{count}
                </span>
                <button
                  type="button"
                  onClick={() => go(1)}
                  disabled={count <= 1}
                  className="rounded p-1 transition-colors hover:bg-slate-700 hover:text-slate-100 disabled:opacity-40 disabled:hover:bg-transparent"
                  aria-label="Next match"
                >
                  <ChevronDownIcon className="size-3.5" />
                </button>
              </div>
            )}
          </div>
        </div>
      )}
      <pre
        ref={preRef}
        className="max-h-[300px] w-full overflow-y-auto bg-slate-800/90 px-4 py-3 text-sm break-all whitespace-pre-wrap text-slate-100"
      >
        {segments}
      </pre>
    </div>
  );
}

/* -----------------------------------------------------------------------------
 * ImageContent - Display base64 encoded images with checkerboard background
 * -------------------------------------------------------------------------- */

function ImageContent({ data }: { data: string }) {
  const image = `data:image/png;base64,${data}`;
  return (
    <div
      className="flex items-center justify-center rounded-lg p-5"
      style={{
        backgroundImage: `linear-gradient(45deg, #ccc 25%, transparent 25%), 
                          linear-gradient(135deg, #ccc 25%, transparent 25%),
                          linear-gradient(45deg, transparent 75%, #ccc 75%),
                          linear-gradient(135deg, transparent 75%, #ccc 75%)`,
        backgroundSize: "25px 25px",
        backgroundPosition: "0 0, 12.5px 0, 12.5px -12.5px, 0px 12.5px",
      }}
    >
      <img src={image} className="max-h-[300px] max-w-full object-contain" />
    </div>
  );
}

/* -----------------------------------------------------------------------------
 * StructuredResultContent - Renders structured content array
 * -------------------------------------------------------------------------- */

function StructuredResultContent({
  content,
}: {
  content: { content: ContentItem[] };
}) {
  return (
    <div className="w-full">
      {content.content.map((item, index) => {
        switch (item.type) {
          case "text": {
            const language = getLanguageFromMimeType(
              item._meta?.["getgram.ai/mime-type"] ?? "text/plain",
            );
            const formattedText = formatTextForLanguage(item.text, language);
            return (
              <SyntaxHighlightedCode
                key={index}
                text={formattedText}
                language={language}
              />
            );
          }
          case "image": {
            return <ImageContent key={index} data={item.data} />;
          }
          default:
            return (
              <pre
                key={index}
                className="px-4 py-3 text-sm break-all whitespace-pre-wrap"
              >
                {JSON.stringify(item, null, 2)}
              </pre>
            );
        }
      })}
    </div>
  );
}

/* -----------------------------------------------------------------------------
 * ToolUISection - Expandable section for Request/Result
 * -------------------------------------------------------------------------- */

function ToolUISection({
  title,
  content,
  defaultExpanded = false,
  highlightSyntax = true,
  language = "json",
  highlight,
  searchActive = false,
}: ToolUISectionProps): React.JSX.Element {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  // For structured content, we don't stringify it
  const isStructured = isStructuredContent(content);
  const contentString = isStructured
    ? JSON.stringify(content, null, 2)
    : typeof content === "string"
      ? content
      : JSON.stringify(content, null, 2);

  const matchCount = highlight?.matches?.length ?? 0;

  let headerIndicator: React.ReactNode = null;
  if (highlight?.headerBadge) headerIndicator = highlight.headerBadge;
  else if (matchCount > 0)
    headerIndicator =
      highlight?.tone === "search" ? (
        <SearchIcon className="size-3.5 text-yellow-500" />
      ) : (
        <TriangleAlertIcon className="size-3.5 text-amber-500" />
      );

  return (
    <div data-slot="tool-ui-section" className="border-t border-border">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="flex w-full cursor-pointer items-center justify-between px-5 py-2.5 text-left transition-colors hover:bg-accent/50"
      >
        <span className="flex items-center gap-2 text-sm text-muted-foreground">
          {title}
          {headerIndicator}
        </span>
        <div className="flex items-center gap-1">
          <CopyButton content={contentString} />
          <ChevronRightIcon
            className={cn(
              "size-4 text-muted-foreground transition-transform duration-200",
              isExpanded && "rotate-90",
            )}
          />
        </div>
      </button>
      {isExpanded && (
        <div className="border-t border-border">
          {matchCount > 0 ? (
            // Flagged content must go through the masked/highlighted view even
            // when it's structured, otherwise secrets render in clear text.
            <HighlightedCode
              text={contentString}
              matches={highlight!.matches}
              masked={highlight?.masked}
              tone={highlight?.tone}
              searchActive={searchActive}
            />
          ) : isStructured ? (
            <StructuredResultContent content={content} />
          ) : highlightSyntax ? (
            <SyntaxHighlightedCode text={contentString} language={language} />
          ) : (
            <pre className="px-4 py-3 text-sm break-all whitespace-pre-wrap text-foreground">
              {contentString}
            </pre>
          )}
        </div>
      )}
    </div>
  );
}

type ApprovalMode = "one-time" | "for-session";

/* -----------------------------------------------------------------------------
 * ToolUI - Main component
 * -------------------------------------------------------------------------- */

// Highlight every case-insensitive occurrence of `query` in a short label (the
// tool name), preserving original casing. Matches over the original string so
// offsets stay aligned; escapes regex metacharacters in the user query.
function highlightLabel(
  text: string,
  query?: string,
  active = false,
): React.ReactNode {
  const q = query?.trim();
  if (!q) return text;
  const re = new RegExp(q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "gi");
  // Active match bright, others pale.
  const markClass = active
    ? "rounded-sm bg-yellow-300/80 px-0.5 text-foreground"
    : "rounded-sm bg-yellow-200/30 px-0.5 text-foreground";
  const nodes: React.ReactNode[] = [];
  let pos = 0;
  let k = 0;
  for (let m = re.exec(text); m !== null; m = re.exec(text)) {
    if (m[0].length === 0) {
      re.lastIndex++;
      continue;
    }
    if (m.index > pos) nodes.push(text.slice(pos, m.index));
    nodes.push(
      <mark key={k++} className={markClass}>
        {m[0]}
      </mark>,
    );
    pos = m.index + m[0].length;
  }
  if (pos === 0) return text;
  if (pos < text.length) nodes.push(text.slice(pos));
  return nodes;
}

function ToolUI({
  name,
  icon,
  provider,
  status = "complete",
  request,
  result,
  defaultExpanded = false,
  requestHighlight,
  resultHighlight,
  nameQuery,
  searchActive = false,
  className,
  annotations,
  onApproveOnce,
  onApproveForSession,
  onDeny,
}: ToolUIProps): React.JSX.Element {
  // Use annotation title if available, otherwise fall back to name
  const displayName = annotations?.title || name;
  const isApprovalPending =
    status === "approval" &&
    onApproveOnce !== undefined &&
    onDeny !== undefined;
  // Auto-expand when approval is pending, collapse when approved
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);
  const hasContent = request !== undefined || result !== undefined;

  // Track approval mode: 'one-time' or 'for-session'
  const [approvalMode, setApprovalMode] = useState<ApprovalMode>("one-time");
  const [isDropdownOpen, setIsDropdownOpen] = useState(false);
  const dropdownTriggerRef = React.useRef<HTMLButtonElement>(null);

  // Collapse when transitioning from approval to non-approval (i.e., when approved/denied)
  useEffect(() => {
    if (!isApprovalPending && isExpanded && !defaultExpanded) {
      setIsExpanded(false);
    }
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- only react to approval transition; defaultExpanded/isExpanded are intentionally not deps
  }, [isApprovalPending]);

  // Handle approve based on selected mode
  const handleApprove = () => {
    if (approvalMode === "for-session" && onApproveForSession) {
      onApproveForSession();
    } else if (onApproveOnce) {
      onApproveOnce();
    }
  };

  return (
    <div
      data-slot="tool-ui"
      className={cn(
        "@container overflow-hidden rounded-lg border border-border bg-card",
        className,
      )}
    >
      {/* Header with provider */}
      {provider && (
        <div
          data-slot="tool-ui-provider"
          className={cn(
            "flex items-center gap-2 border-b border-border px-4 py-2.5",
          )}
        >
          {icon ? (
            <span className="flex size-5 items-center justify-center">
              {icon}
            </span>
          ) : (
            <span className="flex size-5 items-center justify-center rounded bg-muted text-xs font-medium">
              {provider.charAt(0).toUpperCase()}
            </span>
          )}
          <span className="text-sm font-medium">{provider}</span>
        </div>
      )}

      {/* Tool row */}
      <button
        onClick={() => {
          if (hasContent) setIsExpanded(!isExpanded);
        }}
        disabled={!hasContent}
        className={cn(
          "flex w-full items-center gap-2 px-4 py-3 text-left",
          hasContent && "cursor-pointer transition-colors hover:bg-accent/50",
        )}
      >
        <StatusIndicator status={status} />
        <span
          className={cn(
            "flex-1 text-sm",
            !provider && isApprovalPending && "shimmer",
          )}
        >
          {highlightLabel(displayName, nameQuery, searchActive)}
        </span>
        {hasContent && (
          <ChevronDownIcon
            className={cn(
              "size-4 text-muted-foreground transition-transform duration-200",
              isExpanded && "rotate-180",
            )}
          />
        )}
      </button>

      {/* Expandable content */}
      {isExpanded && hasContent && (
        <div data-slot="tool-ui-content">
          {/* When not approval pending, use collapsible section */}
          {request !== undefined && (
            <ToolUISection
              title="Arguments"
              content={request}
              highlightSyntax
              language="json"
              highlight={requestHighlight}
              searchActive={searchActive}
              defaultExpanded={(requestHighlight?.matches?.length ?? 0) > 0}
            />
          )}
          {/* Hide output when approval is pending */}
          {result !== undefined && (
            <ToolUISection
              title="Output"
              content={result}
              highlightSyntax
              language="json"
              highlight={resultHighlight}
              searchActive={searchActive}
              defaultExpanded={(resultHighlight?.matches?.length ?? 0) > 0}
            />
          )}
        </div>
      )}

      {/* Approval actions */}
      {isApprovalPending && (
        <div
          data-slot="tool-ui-approval-actions"
          className="flex flex-col gap-2 border-t border-border px-4 py-3 @[320px]:flex-row @[320px]:items-center @[320px]:justify-end"
        >
          <div className="flex items-center gap-2 @[320px]:mr-auto">
            <span className="text-sm text-muted-foreground">
              This tool requires approval
            </span>
            {annotations?.readOnlyHint && (
              <span className="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                Read-only
              </span>
            )}
            {annotations?.destructiveHint && !annotations?.readOnlyHint && (
              <span className="rounded bg-amber-500/10 px-1.5 py-0.5 text-xs text-amber-600 dark:text-amber-400">
                Destructive
              </span>
            )}
          </div>
          <div className="flex items-center gap-2 self-end">
            <Button
              variant="outline"
              size="sm"
              onClick={onDeny}
              className="text-destructive hover:bg-destructive/10 dark:text-rose-400"
            >
              <XIcon className="size-3 @[320px]:mr-1" />
              <span className="hidden @[320px]:inline">Deny</span>
            </Button>
            {/* Split button: main approve + dropdown for options */}
            <div className="flex items-center">
              <Button
                variant="default"
                size="sm"
                onClick={handleApprove}
                className="flex cursor-pointer justify-between gap-1 rounded-r-none bg-emerald-600 hover:bg-emerald-700"
              >
                <CheckIcon className="mr-1 size-3 dark:text-foreground" />

                <span className="@[320px]:hidden dark:text-foreground">
                  Approve
                </span>
                {/* The min-width is needed to prevent the button from shifting when the text changes */}
                <span className="hidden min-w-[110px] @[320px]:inline dark:text-foreground">
                  {approvalMode === "one-time"
                    ? "Approve this time"
                    : "Approve always"}
                </span>
              </Button>
              <Popover open={isDropdownOpen}>
                <PopoverAnchor asChild>
                  <Button
                    ref={dropdownTriggerRef}
                    variant="default"
                    size="sm"
                    className="cursor-pointer rounded-l-none border-l border-emerald-700 bg-emerald-600 px-2 hover:bg-emerald-700"
                    onClick={() => setIsDropdownOpen((prev) => !prev)}
                  >
                    {isDropdownOpen ? (
                      <ChevronUpIcon className="size-3 dark:text-foreground" />
                    ) : (
                      <ChevronDownIcon className="size-3 dark:text-foreground" />
                    )}
                  </Button>
                </PopoverAnchor>
                <PopoverContent
                  align="end"
                  className="w-fit p-1"
                  sideOffset={4}
                  onInteractOutside={(e) => {
                    // Prevent Radix auto-dismiss to avoid race condition
                    // between DismissableLayer's pointerdown and button's click
                    e.preventDefault();
                    // Use composedPath to detect trigger clicks across Shadow DOM
                    const originalEvent = (
                      e.detail as { originalEvent?: PointerEvent }
                    )?.originalEvent;
                    const path = originalEvent?.composedPath?.() ?? [];
                    if (
                      !path.includes(dropdownTriggerRef.current as EventTarget)
                    ) {
                      // Clicked outside both popover and trigger - close it
                      setIsDropdownOpen(false);
                    }
                    // If clicked on trigger, do nothing - onClick will toggle
                  }}
                  onEscapeKeyDown={() => setIsDropdownOpen(false)}
                >
                  <button
                    onClick={() => {
                      setApprovalMode("one-time");
                      setIsDropdownOpen(false);
                    }}
                    className="relative flex w-full items-start gap-2 rounded-sm px-2 py-2 text-left hover:bg-accent"
                  >
                    <CheckIcon
                      className={cn(
                        "relative top-1 mt-0.5 size-3 shrink-0",
                        approvalMode !== "one-time" && "invisible",
                      )}
                    />
                    <div className="flex flex-col gap-0.5">
                      <span className="text-sm">Approve only once</span>
                      <span className="text-xs text-muted-foreground">
                        You'll be asked again next time
                      </span>
                    </div>
                  </button>
                  {onApproveForSession && (
                    <button
                      onClick={() => {
                        setApprovalMode("for-session");
                        setIsDropdownOpen(false);
                      }}
                      className="relative flex w-full items-start gap-2 rounded-sm px-2 py-2 text-left hover:bg-accent"
                    >
                      <CheckIcon
                        className={cn(
                          "relative top-1 mt-0.5 size-3 shrink-0",
                          approvalMode !== "for-session" && "invisible",
                        )}
                      />
                      <div className="flex flex-col gap-0.5">
                        <span className="text-sm">Approve always</span>
                        <span className="text-xs text-muted-foreground">
                          Trust this tool for the session
                        </span>
                      </div>
                    </button>
                  )}
                </PopoverContent>
              </Popover>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/* -----------------------------------------------------------------------------
 * ToolUIGroup - Container for multiple tool calls
 * -------------------------------------------------------------------------- */

interface ToolUIGroupProps {
  /** Title for the group header */
  title: string;
  /** Optional icon */
  icon?: React.ReactNode;
  /** Overall status of the group */
  status?: "running" | "complete";
  /** Whether the group starts expanded */
  defaultExpanded?: boolean;
  /** Render without the group header, showing children directly. */
  headerless?: boolean;
  /** Child tool UI components */
  children: React.ReactNode;
  /** Additional class names */
  className?: string;
}

function ToolUIGroup({
  title,
  icon,
  status = "complete",
  defaultExpanded = false,
  headerless = false,
  children,
  className,
}: ToolUIGroupProps): React.JSX.Element {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  // A headerless group shows its children unconditionally; when it gains a
  // header mid-stream, start expanded — collapsing would hide content the
  // user was already looking at.
  const [prevHeaderless, setPrevHeaderless] = useState(headerless);
  if (prevHeaderless !== headerless) {
    setPrevHeaderless(headerless);
    if (prevHeaderless) setIsExpanded(true);
  }

  const showChildren = headerless || isExpanded;

  return (
    <div
      data-slot="tool-ui-group"
      className={cn(
        "overflow-hidden rounded-lg border border-border bg-card",
        className,
      )}
    >
      {/* Group header */}
      {!headerless && (
        <button
          onClick={() => setIsExpanded(!isExpanded)}
          aria-expanded={isExpanded}
          className="flex w-full items-center gap-2 px-4 py-3 text-left transition-colors hover:bg-accent/50"
        >
          {icon || (
            <StatusIndicator
              status={status === "running" ? "running" : "complete"}
            />
          )}
          <span
            className={cn(
              "flex-1 text-sm font-medium",
              status === "running" && "shimmer",
            )}
          >
            {title}
          </span>
          <ChevronDownIcon
            className={cn(
              "size-4 text-muted-foreground transition-transform duration-200",
              isExpanded && "rotate-180",
            )}
          />
        </button>
      )}

      {/* Collapsed children are hidden, not unmounted — unmounting would
          reset their state (expansion, async syntax highlighting). */}
      <div
        data-slot="tool-ui-group-content"
        className={cn(
          !headerless && "border-t border-border",
          !showChildren && "hidden",
        )}
      >
        {children}
      </div>
    </div>
  );
}

/* -----------------------------------------------------------------------------
 * Exports
 * -------------------------------------------------------------------------- */

ToolUI.displayName = "ToolUI";
ToolUISection.displayName = "ToolUISection";
SyntaxHighlightedCode.displayName = "SyntaxHighlightedCode";

export {
  ToolUI,
  ToolUISection,
  ToolUIGroup,
  SyntaxHighlightedCode,
  StatusIndicator,
  CopyButton,
};
export type {
  ToolUIProps,
  ToolUISectionProps,
  ToolUIGroupProps,
  ToolStatus,
  ContentItem,
  SectionHighlight,
  SectionMatch,
};
