import {
  type CSSProperties,
  type JSX,
  memo,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { format } from "date-fns";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  ArrowUp,
  Bot,
  ChevronDown,
  ChevronUp,
  Ellipsis,
  GitBranch,
  Loader2,
  ShieldOff,
  SlidersHorizontal,
} from "lucide-react";
import {
  Badge,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
} from "@speakeasy-api/moonshine";
import { MessageContent, type SectionMatch, ToolUI } from "@/elements";
import type { ClaudeToolUsage } from "@gram/client/models/components/claudetoolusage.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
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
import {
  argsToString,
  type DisplayItem,
  messageText,
  type MessageRow,
  type SearchFieldKey,
  type ToolRow,
  type TranscriptRow,
  type TurnAuthor,
} from "./transcript";
import {
  HighlightedMessageText,
  MetaSeparator,
  RevealSecretButton,
  RiskBadge,
} from "./chatRisk";
import {
  distinctRiskCount,
  resultsAreSensitive,
  useRowReveal,
} from "./chatHelpers";
import { QueryHighlight } from "./QueryHighlight";
import { getCategoryCodeForFinding } from "@/pages/security/risk-utils";
import { CreateExclusionContext } from "./exclusionContext";

type RowDecoration = {
  footer?: ReactNode;
};

interface RowContext {
  riskResultsByMessage?: Map<string, RiskResult[]>;
  claudeUsageByMessage?: Map<string, ClaudeUsageMatch>;
  claudeToolUsageByToolUseId?: Map<string, ClaudeToolUsage>;
  rowDecoration?: (messageIds: string[]) => RowDecoration | null;
  /** When the session has findings, non-flagged rows are dimmed to spotlight
   * the risky ones. */
  dimNonRisk: boolean;
  /** Active search query — highlights its occurrences in non-flagged rows and
   * expands tool rows so a match inside a collapsed tool is visible. */
  searchQuery?: string;
  /** Chat-level user label, used as the turn-header name when an individual
   * message carries no user id of its own. */
  userLabel?: string;
  /** Overrides the per-message identity on user turn headers — set when the
   * session ran on a personal AI account, whose email should label the turns
   * instead of the attributed employee's work email. */
  userLabelOverride?: string;
}

type ResolvedRowContext = Required<
  Pick<
    RowContext,
    | "riskResultsByMessage"
    | "claudeUsageByMessage"
    | "claudeToolUsageByToolUseId"
  >
> &
  RowContext;

const EMPTY_RISK_RESULTS = new Map<string, RiskResult[]>();
const EMPTY_CLAUDE_USAGE = new Map<string, ClaudeUsageMatch>();
const EMPTY_CLAUDE_TOOL_USAGE = new Map<string, ClaudeToolUsage>();

function applyRowContextDefaults(ctx: RowContext): ResolvedRowContext {
  return {
    ...ctx,
    riskResultsByMessage: ctx.riskResultsByMessage ?? EMPTY_RISK_RESULTS,
    claudeUsageByMessage: ctx.claudeUsageByMessage ?? EMPTY_CLAUDE_USAGE,
    claudeToolUsageByToolUseId:
      ctx.claudeToolUsageByToolUseId ?? EMPTY_CLAUDE_TOOL_USAGE,
  };
}

function RowDecorationFooter({
  decoration,
  className,
}: {
  decoration: RowDecoration | null | undefined;
  className?: string;
}) {
  if (!decoration?.footer) return null;
  return <div className={className}>{decoration.footer}</div>;
}

function messageIdsForRow(row: TranscriptRow): string[] {
  if (row.kind === "message") return [row.message.id];
  return [row.callMessage?.id, row.resultMessage?.id].filter(
    (id): id is string => Boolean(id),
  );
}

// Fade non-risky rows so the findings stand out.
function dimClass(dim: boolean): string {
  return dim ? "opacity-40" : "";
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

// Two letters for the avatar fallback: the first two name parts of an email
// local-part (jane.doe → JD), else the first two characters.
function userInitials(id: string | undefined): string {
  if (!id) return "?";
  const handle = id.includes("@") ? id.slice(0, id.indexOf("@")) : id;
  const parts = handle.split(/[._\-\s]+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0]![0]! + parts[1]![0]!).toUpperCase();
  return handle.slice(0, 2).toUpperCase();
}

function userDisplayName(id: string | undefined): string {
  return id && id.trim().length > 0 ? id : "User";
}

// A repeating zig-zag (triangle-wave) rule. Drawn as a themeable `bg-border`
// bar revealed through an SVG mask, so it follows the border colour in light and
// dark without baking a colour into the data URI.
const zigzagMask = (strokeWidth: number) =>
  `url("data:image/svg+xml,%3Csvg%20xmlns='http://www.w3.org/2000/svg'%20width='12'%20height='7'%20viewBox='0%200%2012%207'%3E%3Cpath%20d='M0%206%20L6%201%20L12%206'%20fill='none'%20stroke='%23000'%20stroke-width='${strokeWidth}'%20stroke-linejoin='round'/%3E%3C/svg%3E")`;

const zigzagStyle = (strokeWidth: number): CSSProperties => ({
  maskImage: zigzagMask(strokeWidth),
  WebkitMaskImage: zigzagMask(strokeWidth),
  maskRepeat: "repeat-x",
  WebkitMaskRepeat: "repeat-x",
  maskSize: "12px 7px",
  WebkitMaskSize: "12px 7px",
});

// Thin for plain turn dividers; thicker (bold) for the red risk divider.
const ZIGZAG_STYLE = zigzagStyle(0.6);
const ZIGZAG_STYLE_BOLD = zigzagStyle(1);

function ZigZagRule({
  className,
  bold,
}: {
  className?: string;
  bold?: boolean;
}) {
  return (
    <div
      className={cn("h-[7px] flex-1", className ?? "bg-border")}
      style={bold ? ZIGZAG_STYLE_BOLD : ZIGZAG_STYLE}
    />
  );
}

// Distinct, excludable findings for a flagged turn (drops llm_judge and dupes),
// mirroring CreateExclusionButton's selection.
function useActionableExclusions(results: RiskResult[] | undefined) {
  const openCreateExclusion = useContext(CreateExclusionContext);
  const actionable = useMemo(() => {
    if (!openCreateExclusion || !results) return [];
    const seen = new Set<string>();
    const out: RiskResult[] = [];
    for (const r of results) {
      const key = `${r.source}|${r.ruleId ?? ""}|${r.match ?? ""}`;
      if (seen.has(key) || r.ruleId === "llm_judge") continue;
      seen.add(key);
      out.push(r);
    }
    return out;
  }, [results, openCreateExclusion]);
  return { openCreateExclusion, actionable };
}

// Pill-styled (matches the avatar pill) "Actions" dropdown on the turn header,
// surfacing the create-exclusion action for the turn's findings.
function TurnActions({ results }: { results: RiskResult[] }) {
  const { openCreateExclusion, actionable } = useActionableExclusions(results);
  if (actionable.length === 0) return null;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className="bg-background text-muted-foreground hover:text-foreground flex h-9 items-center gap-1.5 rounded-full border px-3 text-sm transition-colors"
        >
          <SlidersHorizontal className="size-3.5" />
          Actions
          <ChevronDown className="size-3.5" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {actionable.map((r) => (
          <DropdownMenuItem
            key={r.id}
            className="cursor-pointer"
            onSelect={() => openCreateExclusion?.(r)}
          >
            <ShieldOff className="size-3.5" />
            Create exclusion
            {actionable.length > 1 &&
              `: ${[r.ruleId, getCategoryCodeForFinding(r.source, r.ruleId)]
                .filter(Boolean)
                .join(" · ")}`}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// Avatar + name above each turn, separated from the previous turn by a rule.
// Both speakers are left-aligned and get an avatar here (outside the bubble) so
// an assistant turn — which may be only tool calls — still reads as one labelled
// block. A flagged user turn shows its risk badge beside the pill and an
// "Actions" menu (create exclusion) on the right.
function TurnHeader({
  author,
  userId,
  userLabel,
  createdAt,
  results,
}: {
  author: TurnAuthor;
  userId?: string;
  userLabel?: string;
  createdAt?: Date;
  results?: RiskResult[];
}) {
  const isUser = author === "user";
  const userName = userId ?? userLabel;
  // Any flagged turn (user or assistant) flags itself via the turn divider — an
  // assistant turn's findings live on its tool rows, aggregated into `results`.
  const flagged = !!results && results.length > 0;
  const riskCount = flagged ? distinctRiskCount(results) : 0;
  // The turn's timestamp sits in the divider (replacing the old "Turn" label).
  const when = createdAt ? format(new Date(createdAt), "MMM d, h:mm a") : null;
  return (
    <div className="px-4">
      {/* Turn separator: a zig-zag rule with a centered label showing the turn's
          date + time (shown for every turn, including the first). A flagged turn
          turns red and appends "N risks" after a divider; that label opens the
          findings popover. */}
      <div
        className={cn(
          "flex items-center gap-3 pt-7 pb-5",
          flagged ? "text-red-800" : "text-muted-foreground",
        )}
      >
        <ZigZagRule
          bold={flagged}
          className={flagged ? "bg-red-800" : undefined}
        />
        <div className="flex items-center gap-2 whitespace-nowrap">
          {when && (
            // No explicit colour: inherit the divider's (red when flagged, muted
            // otherwise) so a risky turn's timestamp matches its "N risks" label.
            <span className="font-mono text-[13px] font-medium uppercase">
              {when}
            </span>
          )}
          {flagged && when && <span className="bg-border h-3.5 w-px" />}
          {flagged && (
            <RiskBadge
              results={results}
              trigger={
                <button
                  type="button"
                  className="inline-flex cursor-pointer items-center gap-1 font-mono text-[13px] font-medium whitespace-nowrap text-red-800 uppercase"
                  onClick={(e) => e.stopPropagation()}
                >
                  {riskCount} {riskCount === 1 ? "risk" : "risks"}
                  <ChevronDown className="size-3.5" />
                </button>
              }
            />
          )}
        </div>
        <ZigZagRule
          bold={flagged}
          className={flagged ? "bg-red-800" : undefined}
        />
      </div>
      <div className="flex items-center justify-between pt-1 pb-3">
        <div className="bg-background flex h-9 min-w-0 items-center gap-2 rounded-full border pr-3 pl-1">
          <Avatar className="size-7 shrink-0">
            <AvatarFallback className="bg-muted text-muted-foreground text-xs font-medium">
              {isUser ? userInitials(userName) : <Bot className="size-3.5" />}
            </AvatarFallback>
          </Avatar>
          <span className="text-foreground max-w-[220px] truncate text-sm font-medium">
            {isUser ? userDisplayName(userName) : "Assistant"}
          </span>
        </div>
        {flagged && <TurnActions results={results} />}
      </div>
    </div>
  );
}

// Outgoing turn — left-aligned to match the assistant, but kept in a bg-muted
// bubble. The avatar/name + risk badge + Actions menu sit in the turn header
// above; the meta strip below keeps the message time, cost, and reveal toggle
// (create-exclusion moved to the header Actions menu).
function UserMessageRow({
  row,
  ctx,
  activeTextOccurrence,
}: {
  row: MessageRow;
  ctx: ResolvedRowContext;
  /** Index of the active search occurrence within this message's text, or null
   * when this row doesn't hold the active occurrence. */
  activeTextOccurrence: number | null;
}) {
  const { message } = row;
  const results = ctx.riskResultsByMessage.get(message.id);
  const usage = ctx.claudeUsageByMessage.get(message.id);
  const text = messageText(message.content);
  const flagged = !!results && results.length > 0;
  const sensitive = flagged && resultsAreSensitive(results);
  const { revealed, setRevealed } = useRowReveal(sensitive);
  const decoration = ctx.rowDecoration?.([message.id]) ?? null;

  return (
    <div
      className={cn(
        "flex flex-col items-start gap-1.5 px-4 py-1.5",
        dimClass(ctx.dimNonRisk && !flagged),
      )}
    >
      <div
        className={cn(
          "bg-muted text-foreground mx-2 max-w-[80%] rounded-xl px-4 py-2 wrap-break-word",
        )}
      >
        {flagged ? (
          <HighlightedMessageText
            text={text}
            results={results}
            revealed={sensitive ? revealed : undefined}
          />
        ) : (
          <div className="whitespace-pre-wrap">
            {ctx.searchQuery ? (
              <QueryHighlight
                text={text}
                query={ctx.searchQuery}
                activeIndex={activeTextOccurrence}
              />
            ) : (
              text
            )}
          </div>
        )}
      </div>
      <RowDecorationFooter
        decoration={decoration}
        className="mx-2 max-w-[80%] pl-4"
      />
      {(usage || sensitive) && (
        <div className="text-muted-foreground mx-2 flex items-center gap-2 pl-4 text-xs">
          {usage && <CostBadge usage={usage} />}
          {usage && sensitive && <MetaSeparator />}
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

// Incoming turn — left, no bubble, markdown body, matching the elements
// <AssistantMessage /> (mx-2, leading-7, no avatar/label).
function AssistantMessageRow({
  row,
  ctx,
  activeTextOccurrence,
}: {
  row: MessageRow;
  ctx: ResolvedRowContext;
  /** Index of the active search occurrence within this message's text, or null
   * when this row doesn't hold the active occurrence. */
  activeTextOccurrence: number | null;
}) {
  const { message } = row;
  const results = ctx.riskResultsByMessage.get(message.id);
  const text = messageText(message.content);
  const flagged = !!results && results.length > 0;
  const sensitive = flagged && resultsAreSensitive(results);
  const { revealed, setRevealed } = useRowReveal(sensitive);
  const decoration = ctx.rowDecoration?.([message.id]) ?? null;

  return (
    <div className={cn("px-4 py-2", dimClass(ctx.dimNonRisk && !flagged))}>
      <div
        className={cn(
          "text-foreground mx-2 min-w-0 leading-relaxed wrap-break-word",
        )}
      >
        {flagged ? (
          <HighlightedMessageText
            text={text}
            results={results}
            revealed={sensitive ? revealed : undefined}
          />
        ) : ctx.searchQuery ? (
          // While searching, render plain (non-markdown) text so query hits can
          // be highlighted inline — markdown output can't carry <mark> spans.
          <div className="whitespace-pre-wrap">
            <QueryHighlight
              text={text}
              query={ctx.searchQuery}
              activeIndex={activeTextOccurrence}
            />
          </div>
        ) : (
          <MessageContent markdown content={text} />
        )}
      </div>
      <RowDecorationFooter
        decoration={decoration}
        className="mx-2 mt-1 max-w-[80%] pl-4"
      />
      {/* Turn-level risk count + exclusion live in the turn header now; the row
          keeps only the reveal toggle for an inline masked value. */}
      {sensitive && (
        <div className="text-muted-foreground mx-2 mt-2 flex items-center gap-2 text-xs">
          <RevealSecretButton
            results={results}
            revealed={revealed}
            onToggle={() => setRevealed(!revealed)}
          />
        </div>
      )}
    </div>
  );
}

function SystemMessageRow({
  row,
  ctx,
}: {
  row: MessageRow;
  ctx: ResolvedRowContext;
}) {
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

function MessageRowView({
  row,
  ctx,
  activeTextOccurrence,
}: {
  row: MessageRow;
  ctx: ResolvedRowContext;
  activeTextOccurrence: number | null;
}) {
  switch (row.entryType) {
    case "user":
      return (
        <UserMessageRow
          row={row}
          ctx={ctx}
          activeTextOccurrence={activeTextOccurrence}
        />
      );
    case "assistant":
      return (
        <AssistantMessageRow
          row={row}
          ctx={ctx}
          activeTextOccurrence={activeTextOccurrence}
        />
      );
    case "system":
      return <SystemMessageRow row={row} ctx={ctx} />;
  }
}

function toolResults(
  row: ToolRow,
  ctx: ResolvedRowContext,
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

function ToolRowView({
  row,
  ctx,
  active,
  activeNameOccurrence,
  activeArgsOccurrence,
  activeOutputOccurrence,
}: {
  row: ToolRow;
  ctx: ResolvedRowContext;
  /** This tool holds the active search occurrence → expand it (and remount on
   * toggle so it collapses again when navigation moves to another match). */
  active: boolean;
  /** Active occurrence index within the tool name, or null. */
  activeNameOccurrence: number | null;
  /** Active occurrence index within the Arguments section, or null. */
  activeArgsOccurrence: number | null;
  /** Active occurrence index within the Output section, or null. */
  activeOutputOccurrence: number | null;
}) {
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
  // Search: which of this tool's sections contain the query (case-insensitive,
  // mirroring the server's ILIKE) — drives which section auto-opens + highlights.
  const queryLc = ctx.searchQuery?.trim().toLowerCase();
  const requestMatches =
    !!queryLc && (request?.toLowerCase().includes(queryLc) ?? false);
  const resultMatches =
    !!queryLc && (result?.toLowerCase().includes(queryLc) ?? false);
  // Each tool row is a distinct display item, so the active occurrence's row
  // index already identifies exactly this tool — no need to re-check which
  // section matched (that's what the per-field active indices below carry).
  const toolActive = active;
  const searchSection: SectionMatch[] = ctx.searchQuery
    ? [{ value: ctx.searchQuery, label: "match" }]
    : [];
  const requestHighlight = callResults?.length
    ? {
        matches: reqMatches ?? [],
        masked: resultsAreSensitive(callResults),
        headerBadge: <RiskBadge results={callResults} />,
      }
    : requestMatches
      ? {
          matches: searchSection,
          masked: false,
          tone: "search" as const,
          activeOccurrence: activeArgsOccurrence,
        }
      : undefined;
  const resultHighlight = resultResults?.length
    ? {
        matches: resMatches ?? [],
        masked: resultsAreSensitive(resultResults),
        headerBadge: <RiskBadge results={resultResults} />,
      }
    : resultMatches
      ? {
          matches: searchSection,
          masked: false,
          tone: "search" as const,
          activeOccurrence: activeOutputOccurrence,
        }
      : undefined;

  const toolUseId = row.toolCall?.id ?? row.resultMessage?.toolCallId ?? "";
  const usage = ctx.claudeToolUsageByToolUseId.get(toolUseId);
  const inputBytes = usage?.inputSizeBytes ?? 0;
  const outputBytes = usage?.resultSizeBytes ?? 0;
  const decoration = ctx.rowDecoration?.(messageIdsForRow(row)) ?? null;

  return (
    <div className={cn("px-4 py-2.5", dimClass(ctx.dimNonRisk && !flagged))}>
      {inputBytes + outputBytes > 0 && (
        <div className="mb-1.5 flex items-center gap-2 pl-1">
          <ToolByteBadge bytes={inputBytes + outputBytes} />
        </div>
      )}
      <ToolUI
        // ToolUI expansion is uncontrolled, so key on `toolActive` to remount it:
        // landing on this tool's match opens it; moving to the next match
        // collapses it again. Non-active tools keep a stable key (and any manual
        // expansion the user made).
        key={toolActive ? "active" : "default"}
        name={name}
        request={request}
        result={result}
        status="complete"
        defaultExpanded={flagged || toolActive}
        requestHighlight={requestHighlight}
        resultHighlight={resultHighlight}
        nameQuery={ctx.searchQuery}
        nameActiveOccurrence={activeNameOccurrence}
      />
      <RowDecorationFooter
        decoration={decoration}
        className="mt-1 max-w-[80%] pl-4"
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
  /** Search match navigation: the display-item index to center, re-issued every
   * time `scrollNonce` changes (so repeatedly hitting next/prev re-scrolls even
   * to the same index). null when not navigating matches. */
  scrollToItemIndex?: number | null;
  scrollNonce?: number;
  /** The currently-active search occurrence: which display item, which field of
   * that row, and which occurrence within the field. The row holding it expands
   * (a matched tool opens) and renders that single occurrence bright; everything
   * else is pale. null when not searching / no matches. */
  activeOccurrence?: {
    itemIndex: number;
    fieldKey: SearchFieldKey;
    indexInField: number;
  } | null;
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

// Memoized row subtree. The transcript re-renders on every match-navigation
// (scrollNonce/scrollToItemIndex churn in `pagination`), but a row's content
// only depends on (row, ctx) — both stable across navigation — so this skips
// re-rendering the expensive ToolUI on each next/prev.
/** The query field + occurrence index the global navigator is currently on,
 * within the row this is passed to (null for every non-active row). */
interface ActiveField {
  key: SearchFieldKey;
  index: number;
}

const RowView = memo(function RowView({
  row,
  ctx,
  active,
  activeField,
}: {
  row: TranscriptRow;
  ctx: ResolvedRowContext;
  /** This row holds the currently-active search occurrence (drives tool expand). */
  active: boolean;
  /** Which field + occurrence in this row is active, or null when none is. */
  activeField: ActiveField | null;
}) {
  if (row.kind === "message") {
    return (
      <MessageRowView
        row={row}
        ctx={ctx}
        activeTextOccurrence={
          activeField?.key === "text" ? activeField.index : null
        }
      />
    );
  }
  return (
    <ToolRowView
      row={row}
      ctx={ctx}
      active={active}
      activeNameOccurrence={
        activeField?.key === "name" ? activeField.index : null
      }
      activeArgsOccurrence={
        activeField?.key === "args" ? activeField.index : null
      }
      activeOutputOccurrence={
        activeField?.key === "output" ? activeField.index : null
      }
    />
  );
});

function DisplayItemView({
  item,
  index,
  ctx,
  pagination,
}: {
  item: DisplayItem;
  /** This item's position in the display list — matched against the active
   * occurrence's itemIndex to decide if this row holds it. */
  index: number;
  ctx: ResolvedRowContext;
  pagination: TranscriptPagination;
}) {
  switch (item.type) {
    case "divider":
      return <SegmentDivider generation={item.generation} />;
    case "turnHeader":
      return (
        <TurnHeader
          author={item.author}
          userId={ctx.userLabelOverride ?? item.userId}
          userLabel={ctx.userLabel}
          createdAt={item.createdAt}
          results={item.messageIds.flatMap(
            (id) => ctx.riskResultsByMessage.get(id) ?? [],
          )}
        />
      );
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
    case "row": {
      // Only the row holding the active occurrence is "active"; computed here
      // (not in ctx) so navigation re-renders just the toggling rows, not all of
      // them. The active row gets the field + occurrence index to render bright;
      // every other row gets null and stays pale (and skips re-render via memo).
      const occ = pagination.activeOccurrence;
      const active = occ != null && occ.itemIndex === index;
      const activeField: ActiveField | null = active
        ? { key: occ.fieldKey, index: occ.indexInField }
        : null;
      return (
        <RowView
          row={item.row}
          ctx={ctx}
          active={active}
          activeField={activeField}
        />
      );
    }
  }
}

export function ChatTranscript({
  items,
  ctx: rawCtx,
  pagination,
  emptyMessage = "No messages to display.",
}: {
  items: DisplayItem[];
  ctx: RowContext;
  pagination: TranscriptPagination;
  emptyMessage?: string;
}): JSX.Element {
  const ctx = useMemo(() => applyRowContextDefaults(rawCtx), [rawCtx]);
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
    scrollToItemIndex,
    scrollNonce,
  } = pagination;

  // "Start of thread" affordance: shown once the reader has scrolled a screenful
  // down from the top. Only meaningful in the normal (non-risk) view, where the
  // first item is the thread's true first message — in risk mode index 0 is the
  // top of the finding window, not the start.
  const [scrolledDown, setScrolledDown] = useState(false);

  // Preserve scroll position across a prepend: capture distance-from-bottom
  // before fetching older messages, restore it once the list grows.
  const anchorRef = useRef<number | null>(null);
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    setScrolledDown(el.scrollTop > 400);
    if (el.scrollTop < 200 && hasMoreBefore) {
      anchorRef.current = el.scrollHeight - el.scrollTop;
      onLoadOlder();
    }
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distanceFromBottom < 200 && hasMoreAfter) onLoadNewer();
  }, [hasMoreBefore, hasMoreAfter, onLoadOlder, onLoadNewer]);

  // Jump back to the very first message. index 0 is always loaded (the from-start
  // page is the initial page and the list only grows forward), so this is an
  // instant scroll, no fetch. Reuse the virtualizer's align:"start".
  const scrollToStart = useCallback(() => {
    virtualizer.scrollToIndex(0, { align: "start" });
  }, [virtualizer]);
  const showStartButton = scrolledDown && !scrollToFinding && items.length > 0;

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

  // Search match navigation: center the target row each time the nonce changes
  // (next/prev), re-issuing across a few frames so the estimate→measured height
  // shift doesn't leave it off-screen. Matches are always loaded by construction
  // (the windowed response includes every match), so the index resolves at once.
  useEffect(() => {
    if (scrollToItemIndex == null || scrollToItemIndex >= items.length) return;
    if (!scrollRef.current) return;
    let raf = 0;
    let tries = 0;
    const settle = () => {
      virtualizer.scrollToIndex(scrollToItemIndex, { align: "center" });
      tries += 1;
      if (tries < 12) raf = requestAnimationFrame(settle);
    };
    raf = requestAnimationFrame(settle);
    return () => cancelAnimationFrame(raf);
  }, [scrollNonce, scrollToItemIndex, items.length, virtualizer]);

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
    <div className="relative flex min-h-0 flex-1 flex-col">
      {showStartButton && (
        <button
          type="button"
          onClick={scrollToStart}
          className="bg-background text-muted-foreground hover:text-foreground hover:bg-muted absolute top-2 left-1/2 z-10 inline-flex -translate-x-1/2 items-center gap-1 rounded-full border px-2.5 py-1 text-xs shadow-sm transition-colors"
        >
          <ArrowUp className="size-3" />
          Start of thread
        </button>
      )}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="min-h-0 flex-1 overflow-y-auto pt-4 pb-10"
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
                index={virtualRow.index}
                ctx={ctx}
                pagination={pagination}
              />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export type { RowContext };
