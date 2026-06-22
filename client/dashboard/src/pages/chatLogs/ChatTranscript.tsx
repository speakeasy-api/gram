import {
  type JSX,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useRef,
} from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  ChevronDown,
  ChevronUp,
  Ellipsis,
  GitBranch,
  Loader2,
} from "lucide-react";
import { Badge, Icon } from "@speakeasy-api/moonshine";
import { MessageContent, type SectionMatch, ToolUI } from "@gram-ai/elements";
import type {
  ClaudeToolUsage,
  RiskResult,
} from "@gram/client/models/components";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  type ClaudeUsageMatch,
  formatByteCount,
  formatDurationFromNanos,
  formatUsageCost,
} from "./claudeUsage";
import { type DisplayItem, type MessageRow, type ToolRow } from "./transcript";
import {
  CreateExclusionButton,
  HighlightedMessageText,
  MetaSeparator,
  RevealSecretButton,
  RiskBadge,
} from "./chatRisk";
import { resultsAreSensitive, useRowReveal } from "./chatRiskHelpers";
import { CreateExclusionContext } from "./exclusionContext";

interface RowContext {
  riskResultsByMessage: Map<string, RiskResult[]>;
  claudeUsageByMessage: Map<string, ClaudeUsageMatch>;
  claudeToolUsageByToolUseId: Map<string, ClaudeToolUsage>;
  /** When the session has findings, non-flagged rows are dimmed to spotlight
   * the risky ones. */
  dimNonRisk: boolean;
}

// Fade non-risky rows so the findings stand out.
function dimClass(dim: boolean): string {
  return dim ? "opacity-40" : "";
}

// Strip the injected `<message-context>…</message-context>` envelope (event id,
// timestamp, user id) and trailing whitespace the harness prepends to prompts —
// it's machine plumbing, not part of the conversation.
function cleanMessageText(raw: string): string {
  return (
    raw
      // Only the leading injected envelope, not literal tags mid-message.
      .replace(/^\s*<message-context>[\s\S]*?<\/message-context>/i, "")
      .replace(/[ \t]+$/gm, "")
      .trim()
  );
}

function messageText(content: unknown): string {
  if (typeof content === "string") return cleanMessageText(content);
  if (Array.isArray(content)) {
    return cleanMessageText(
      content
        .map((part) =>
          part &&
          typeof part === "object" &&
          "text" in part &&
          typeof (part as { text: unknown }).text === "string"
            ? (part as { text: string }).text
            : "",
        )
        .filter(Boolean)
        .join("\n"),
    );
  }
  if (content == null) return "";
  return JSON.stringify(content, null, 2);
}

function argsToString(args: string | object | undefined): string | undefined {
  if (args === undefined) return undefined;
  return typeof args === "string" ? args : JSON.stringify(args, null, 2);
}

function CostBadge({ usage }: { usage: ClaudeUsageMatch }) {
  const { turn, match } = usage;
  const duration = formatDurationFromNanos(
    turn.startTimeUnixNano,
    turn.endTimeUnixNano,
  );
  const rows: Array<[string, string]> = [
    ["Input", turn.inputTokens.toLocaleString()],
    ["Output", turn.outputTokens.toLocaleString()],
    ["Cache read", turn.cacheReadTokens.toLocaleString()],
    ["Cache creation", turn.cacheCreationTokens.toLocaleString()],
    ["Total tokens", turn.totalTokens.toLocaleString()],
    ["Cost", formatUsageCost(turn.costUsd)],
    ["Requests", turn.requestCount.toLocaleString()],
    ["Models", turn.models.length > 0 ? turn.models.join(", ") : "unknown"],
    ...(duration ? [["Duration", duration] as [string, string]] : []),
    ...(match === "ordered"
      ? [["Match", "estimated by turn order"] as [string, string]]
      : []),
  ];

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground shrink-0 cursor-pointer text-xs tabular-nums transition-colors"
          onClick={(e) => e.stopPropagation()}
        >
          {formatUsageCost(turn.costUsd)}
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72">
        <div className="space-y-3">
          <div>
            <div className="text-sm font-semibold">Turn cost</div>
            <div className="text-muted-foreground font-mono text-[11px]">
              {turn.promptId}
            </div>
          </div>
          <div className="divide-border divide-y">
            {rows.map(([label, value]) => (
              <div
                key={label}
                className="flex items-start justify-between gap-3 py-1.5 text-xs"
              >
                <span className="text-muted-foreground">{label}</span>
                <span className="max-w-44 text-right font-medium break-words">
                  {value}
                </span>
              </div>
            ))}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function ToolByteBadge({ bytes }: { bytes: number }) {
  if (bytes <= 0) return null;
  return (
    <Badge variant="neutral" className="shrink-0 text-[10px]">
      <Badge.Text>{formatByteCount(bytes)}</Badge.Text>
    </Badge>
  );
}

// Outgoing turn — right-aligned bubble, matching the elements <UserMessage />
// (bg-muted, rounded-xl). When flagged, the risk/cost badges and the
// create-exclusion action ride above the bubble (right-aligned) so the bubble
// itself stays clean.
// Two letters for the avatar fallback: the first two name parts of an email
// local-part (jane.doe → JD), else the first two characters.
function userInitials(id: string | undefined): string {
  if (!id) return "?";
  const handle = id.includes("@") ? id.slice(0, id.indexOf("@")) : id;
  const parts = handle.split(/[._\-\s]+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0]![0]! + parts[1]![0]!).toUpperCase();
  return handle.slice(0, 2).toUpperCase();
}

function UserMessageRow({ row, ctx }: { row: MessageRow; ctx: RowContext }) {
  const { message } = row;
  const results = ctx.riskResultsByMessage.get(message.id);
  const usage = ctx.claudeUsageByMessage.get(message.id);
  const text = messageText(message.content);
  const flagged = !!results && results.length > 0;
  const sensitive = flagged && resultsAreSensitive(results);
  const { revealed, setRevealed } = useRowReveal(sensitive);

  return (
    <div
      className={cn(
        "flex flex-col items-end gap-2 px-4 py-2",
        dimClass(ctx.dimNonRisk && !flagged),
      )}
    >
      {(flagged || usage) && (
        <div className="text-muted-foreground flex items-center gap-2 pr-1 text-xs">
          {flagged && <RiskBadge results={results} compact />}
          {flagged && usage && <MetaSeparator />}
          {usage && <CostBadge usage={usage} />}
          {flagged && (
            <CreateExclusionButton results={results} leadingSeparator />
          )}
          {sensitive && (
            <RevealSecretButton
              results={results}
              revealed={revealed}
              onToggle={() => setRevealed(!revealed)}
            />
          )}
        </div>
      )}
      <div className="bg-muted text-foreground flex max-w-[80%] items-center gap-2.5 rounded-xl py-2 pr-4 pl-2 wrap-break-word">
        <Avatar className="size-8 shrink-0 self-start">
          <AvatarFallback className="bg-background text-xs font-medium">
            {userInitials(message.externalUserId ?? message.userId)}
          </AvatarFallback>
        </Avatar>
        <div className="min-w-0">
          {flagged ? (
            <HighlightedMessageText
              text={text}
              results={results}
              revealed={sensitive ? revealed : undefined}
            />
          ) : (
            <div className="whitespace-pre-wrap">{text}</div>
          )}
        </div>
      </div>
    </div>
  );
}

// Incoming turn — left, no bubble, markdown body, matching the elements
// <AssistantMessage /> (mx-2, leading-7, no avatar/label).
function AssistantMessageRow({
  row,
  ctx,
}: {
  row: MessageRow;
  ctx: RowContext;
}) {
  const { message } = row;
  const results = ctx.riskResultsByMessage.get(message.id);
  const text = messageText(message.content);
  const flagged = !!results && results.length > 0;
  const sensitive = flagged && resultsAreSensitive(results);
  const { revealed, setRevealed } = useRowReveal(sensitive);

  return (
    <div className={cn("px-4 py-2", dimClass(ctx.dimNonRisk && !flagged))}>
      <div className="text-foreground mx-2 min-w-0 leading-relaxed wrap-break-word">
        {flagged ? (
          <HighlightedMessageText
            text={text}
            results={results}
            revealed={sensitive ? revealed : undefined}
          />
        ) : (
          <MessageContent markdown content={text} />
        )}
      </div>
      {flagged && (
        <div className="text-muted-foreground mx-2 mt-2 flex items-center gap-2 text-xs">
          <RiskBadge results={results} compact />
          <CreateExclusionButton results={results} leadingSeparator />
          {sensitive && (
            <RevealSecretButton
              results={results}
              revealed={revealed}
              onToggle={() => setRevealed(!revealed)}
            />
          )}
        </div>
      )}
    </div>
  );
}

function SystemMessageRow({ row, ctx }: { row: MessageRow; ctx: RowContext }) {
  const text = messageText(row.message.content);
  return (
    <div className={cn("px-4 py-2", dimClass(ctx.dimNonRisk))}>
      <details className="border-muted bg-muted/20 group overflow-hidden rounded-md border">
        <summary className="text-muted-foreground hover:bg-muted/40 flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-xs select-none">
          <Icon
            name="chevron-right"
            className="size-3 transition-transform group-open:rotate-90"
          />
          <Icon name="settings" className="size-3" />
          <span>System prompt</span>
        </summary>
        <div className="border-muted border-t p-3 font-mono text-xs whitespace-pre-wrap">
          {text}
        </div>
      </details>
    </div>
  );
}

function MessageRowView({ row, ctx }: { row: MessageRow; ctx: RowContext }) {
  switch (row.entryType) {
    case "user":
      return <UserMessageRow row={row} ctx={ctx} />;
    case "assistant":
      return <AssistantMessageRow row={row} ctx={ctx} />;
    case "system":
      return <SystemMessageRow row={row} ctx={ctx} />;
  }
}

function toolResults(
  row: ToolRow,
  ctx: RowContext,
): { callResults?: RiskResult[]; resultResults?: RiskResult[] } {
  return {
    callResults: row.callMessage
      ? ctx.riskResultsByMessage.get(row.callMessage.id)
      : undefined,
    resultResults: row.resultMessage
      ? ctx.riskResultsByMessage.get(row.resultMessage.id)
      : undefined,
  };
}

// Distinct findings (by matched value) for one tool section, each carrying its
// rule label and a context-wired "create exclusion" action. The elements
// ToolUI surfaces the active match's label + action as you step through them.
function toSectionMatches(
  results: RiskResult[] | undefined,
  openExclusion: ((r: RiskResult) => void) | null,
): SectionMatch[] | undefined {
  if (!results?.length) return undefined;
  const byValue = new Map<string, RiskResult>();
  for (const r of results) {
    if (r.match && !byValue.has(r.match)) byValue.set(r.match, r);
  }
  if (byValue.size === 0) return undefined;
  return [...byValue.values()]
    .sort((a, b) => (b.match?.length ?? 0) - (a.match?.length ?? 0))
    .map((r) => ({
      value: r.match!,
      label: r.ruleId && r.ruleId !== "llm_judge" ? r.ruleId : r.source,
      onExclude:
        openExclusion && r.ruleId !== "llm_judge"
          ? () => openExclusion(r)
          : undefined,
    }));
}

function ToolRowView({ row, ctx }: { row: ToolRow; ctx: RowContext }) {
  const openExclusion = useContext(CreateExclusionContext);
  const name =
    row.toolCall?.function?.name || row.toolCall?.name || "Tool result";
  const request = argsToString(row.toolCall?.function?.arguments);
  const result = row.resultMessage
    ? messageText(row.resultMessage.content)
    : undefined;

  const { callResults, resultResults } = toolResults(row, ctx);
  const flagged =
    (callResults?.length ?? 0) > 0 || (resultResults?.length ?? 0) > 0;

  // Flag matches inside the tool's own Arguments/Output sections; the elements
  // ToolUI draws the risk badge in the section header, plus the match navigator
  // and active-match exclusion action.
  const reqMatches = toSectionMatches(callResults, openExclusion);
  const resMatches = toSectionMatches(resultResults, openExclusion);
  const requestHighlight = callResults?.length
    ? {
        matches: reqMatches ?? [],
        masked: resultsAreSensitive(callResults),
        headerBadge: <RiskBadge results={callResults} />,
      }
    : undefined;
  const resultHighlight = resultResults?.length
    ? {
        matches: resMatches ?? [],
        masked: resultsAreSensitive(resultResults),
        headerBadge: <RiskBadge results={resultResults} />,
      }
    : undefined;

  const toolUseId = row.toolCall?.id ?? row.resultMessage?.toolCallId ?? "";
  const usage = ctx.claudeToolUsageByToolUseId.get(toolUseId);
  const inputBytes = usage?.inputSizeBytes ?? 0;
  const outputBytes = usage?.resultSizeBytes ?? 0;

  return (
    <div className={cn("px-4 py-2.5", dimClass(ctx.dimNonRisk && !flagged))}>
      {inputBytes + outputBytes > 0 && (
        <div className="mb-1.5 flex items-center gap-2 pl-1">
          <ToolByteBadge bytes={inputBytes + outputBytes} />
        </div>
      )}
      <ToolUI
        name={name}
        request={request}
        result={result}
        status="complete"
        defaultExpanded={flagged}
        requestHighlight={requestHighlight}
        resultHighlight={resultHighlight}
      />
    </div>
  );
}

function SegmentDivider({ generation }: { generation: number }) {
  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="bg-border h-px flex-1" />
      <span className="text-muted-foreground flex items-center gap-1.5 text-xs">
        <GitBranch className="size-3" />
        Conversation segment {generation + 1}
      </span>
      <div className="bg-border h-px flex-1" />
    </div>
  );
}

/** Pagination + gap-loading wiring the transcript drives from its own scroll. */
export interface TranscriptPagination {
  hasMoreBefore: boolean;
  hasMoreAfter: boolean;
  onLoadOlder: () => void;
  onLoadNewer: () => void;
  isFetchingOlder: boolean;
  isFetchingNewer: boolean;
  onLoadGap?: (afterSeq: number) => void;
  isLoadingGap?: (afterSeq: number) => boolean;
  /** Display-item index to bring to the top on first paint, or null to stay at
   * the top. Normal view → null (first message in view); risk view → the first
   * flagged row. */
  initialScrollIndex: number | null;
  /** Risk/scroll-to-finding view: the transcript opens jumped to the first
   * finding instead of paginating from the top, so the top-of-list auto-load
   * must stay off (otherwise it walks the window back to the chat start before
   * the finding index — sourced from a separate query — has resolved). */
  scrollToFinding: boolean;
}

/** Edge "load older/newer" affordance + the risk-gap "load in-between" marker. */
function LoadDivider({
  icon,
  label,
  loading,
  onClick,
}: {
  icon: "up" | "down" | "ellipsis";
  label: string;
  loading: boolean;
  onClick: () => void;
}) {
  const Glyph =
    icon === "up" ? ChevronUp : icon === "down" ? ChevronDown : Ellipsis;
  return (
    <div className="flex items-center justify-center gap-2 px-4 py-2">
      <div className="bg-border h-px flex-1" />
      <button
        type="button"
        disabled={loading}
        onClick={onClick}
        className="text-muted-foreground hover:text-foreground hover:bg-muted/50 inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs transition-colors disabled:opacity-60"
      >
        {loading ? (
          <Loader2 className="size-3 animate-spin" />
        ) : (
          <Glyph className="size-3" />
        )}
        {label}
      </button>
      <div className="bg-border h-px flex-1" />
    </div>
  );
}

function DisplayItemView({
  item,
  ctx,
  pagination,
}: {
  item: DisplayItem;
  ctx: RowContext;
  pagination: TranscriptPagination;
}) {
  switch (item.type) {
    case "divider":
      return <SegmentDivider generation={item.generation} />;
    case "loadMore":
      return item.dir === "older" ? (
        <LoadDivider
          icon="up"
          label="Load older messages"
          loading={pagination.isFetchingOlder}
          onClick={pagination.onLoadOlder}
        />
      ) : (
        <LoadDivider
          icon="down"
          label="Load newer messages"
          loading={pagination.isFetchingNewer}
          onClick={pagination.onLoadNewer}
        />
      );
    case "serverGap":
      return (
        <LoadDivider
          icon="ellipsis"
          label="Load messages in between"
          loading={pagination.isLoadingGap?.(item.afterSeq) ?? false}
          onClick={() => pagination.onLoadGap?.(item.afterSeq)}
        />
      );
    case "row":
      return item.row.kind === "message" ? (
        <MessageRowView row={item.row} ctx={ctx} />
      ) : (
        <ToolRowView row={item.row} ctx={ctx} />
      );
  }
}

export function ChatTranscript({
  items,
  ctx,
  pagination,
  emptyMessage = "No messages to display.",
}: {
  items: DisplayItem[];
  ctx: RowContext;
  pagination: TranscriptPagination;
  emptyMessage?: string;
}): JSX.Element {
  const scrollRef = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 72,
    overscan: 8,
    getItemKey: (index) => items[index]!.id,
  });

  const {
    hasMoreBefore,
    hasMoreAfter,
    onLoadOlder,
    onLoadNewer,
    initialScrollIndex,
    scrollToFinding,
  } = pagination;

  // Preserve scroll position across a prepend: capture distance-from-bottom
  // before fetching older messages, restore it once the list grows.
  const anchorRef = useRef<number | null>(null);
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (el.scrollTop < 200 && hasMoreBefore) {
      anchorRef.current = el.scrollHeight - el.scrollTop;
      onLoadOlder();
    }
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distanceFromBottom < 200 && hasMoreAfter) onLoadNewer();
  }, [hasMoreBefore, hasMoreAfter, onLoadOlder, onLoadNewer]);

  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el && anchorRef.current !== null) {
      el.scrollTop = el.scrollHeight - anchorRef.current;
      anchorRef.current = null;
    }
  }, [items.length]);

  // Opening already at the top (scrollTop 0) means the scroll handler can't
  // fire, so backward infinite-scroll would stall on the first page. Pull the
  // previous page whenever we settle near the top with more above. Never in the
  // scroll-to-finding view: there the finding index arrives from a separate
  // query, so until it resolves this would otherwise walk the window all the way
  // back to the chat start instead of jumping to the finding. The prepend anchor
  // pushes scrollTop past the threshold once a page lands, so this self-limits.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || scrollToFinding) return;
    if (hasMoreBefore && el.scrollTop < 200) {
      anchorRef.current = el.scrollHeight - el.scrollTop;
      onLoadOlder();
    }
  }, [hasMoreBefore, items.length, scrollToFinding, onLoadOlder]);

  // Land on the requested row once the first page is laid out: the top for a
  // normal session (null → no scroll), or the first finding in risk view.
  // scrollToIndex under-shoots a far-down target because unmeasured rows use the
  // 72px estimate while tool UIs are much taller — so re-issue it across a few
  // frames, letting dynamic measurements converge on the real offset, then lock.
  const didInitialScroll = useRef(false);
  useEffect(() => {
    if (initialScrollIndex == null || didInitialScroll.current) return;
    if (items.length === 0 || !scrollRef.current) return;
    if (initialScrollIndex >= items.length) return;
    let raf = 0;
    let tries = 0;
    const settle = () => {
      virtualizer.scrollToIndex(initialScrollIndex, { align: "start" });
      tries += 1;
      if (tries < 12) {
        raf = requestAnimationFrame(settle);
      } else {
        didInitialScroll.current = true;
      }
    };
    raf = requestAnimationFrame(settle);
    return () => cancelAnimationFrame(raf);
  }, [initialScrollIndex, items.length, virtualizer]);

  if (items.length === 0) {
    return (
      <div className="flex-1 overflow-y-auto">
        <div className="text-muted-foreground p-6 text-center text-sm">
          {emptyMessage}
        </div>
      </div>
    );
  }

  return (
    <div
      ref={scrollRef}
      onScroll={handleScroll}
      className="flex-1 overflow-y-auto pt-4 pb-10"
    >
      <div
        className="relative w-full"
        style={{ height: `${virtualizer.getTotalSize()}px` }}
      >
        {virtualizer.getVirtualItems().map((virtualRow) => (
          <div
            key={virtualRow.key}
            data-index={virtualRow.index}
            ref={virtualizer.measureElement}
            className="absolute top-0 left-0 w-full"
            style={{ transform: `translateY(${virtualRow.start}px)` }}
          >
            <DisplayItemView
              item={items[virtualRow.index]!}
              ctx={ctx}
              pagination={pagination}
            />
          </div>
        ))}
      </div>
    </div>
  );
}

export type { RowContext };
