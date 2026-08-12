/**
 * Effects view — a condensed session transcript focused on *what happened*
 * rather than what was said. Tool calls and risk events are shown in full;
 * user / assistant messages are collapsed to a 2-line inline preview with a
 * hover-revealed "show more" overlay. No turn headers — the tiny per-row icon
 * is enough to distinguish speaker.
 */
import {
  type JSX,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Bot, Loader2, Wrench } from "lucide-react";
import { cn } from "@/lib/utils";
import { ToolUIGroup, ToolUISection } from "@/elements";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import {
  argsToString,
  messageText,
  type MessageRow,
  type ToolRow,
  type TranscriptRow,
} from "./transcript";
import { RiskBadge, RevealSecretButton } from "./chatRisk";
import { resultsAreSensitive, useRowReveal } from "./chatHelpers";
import { toolSectionRiskMatches, type ToolRiskField } from "./toolRisk";
import { CreateExclusionContext } from "./exclusionContext";
import { useDismissFinding } from "@/pages/security/useDismissFinding";
import type { SectionMatch } from "@/elements";
import type { ClaudeToolUsage } from "@gram/client/models/components/claudetoolusage.js";
import type { ClaudeTurnUsage } from "@gram/client/models/components/claudeturnusage.js";
import { userInitials } from "./chatTranscriptUtils";
import { useSummarizeToolCallMutation } from "@gram/client/react-query/summarizeToolCall.js";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface EffectsViewContext {
  chatId: string;
  riskResultsByMessage: Map<string, RiskResult[]>;
  claudeToolUsageByToolUseId: Map<string, ClaudeToolUsage>;
  claudeTurnByPromptId: Map<string, ClaudeTurnUsage>;
  userLabel?: string;
  userLabelOverride?: string;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function toSectionRisk(
  results: RiskResult[] | undefined,
  content: string | undefined,
  field: ToolRiskField,
  openExclusion: ((r: RiskResult) => void) | null,
  dismiss: (results: RiskResult[]) => void,
): { matches: SectionMatch[]; results: RiskResult[] } | undefined {
  const sectionMatches = toolSectionRiskMatches(results, content, field);
  if (sectionMatches.length === 0) return undefined;
  const matchingResults = new Map<string, RiskResult>();
  const matches = sectionMatches.map(({ value, result }) => {
    matchingResults.set(result.id, result);
    return {
      value,
      label:
        result.ruleId && result.ruleId !== "llm_judge"
          ? result.ruleId
          : result.source,
      onExclude:
        openExclusion && result.ruleId !== "llm_judge"
          ? () => openExclusion(result)
          : undefined,
      onMarkFalsePositive: () => dismiss([result]),
    };
  });
  return { matches, results: [...matchingResults.values()] };
}

// ---------------------------------------------------------------------------
// Compact message row
// ---------------------------------------------------------------------------

function CompactMessageRow({
  row,
  ctx,
}: {
  row: MessageRow;
  ctx: EffectsViewContext;
}) {
  const [expanded, setExpanded] = useState(false);
  const [truncatable, setTruncatable] = useState(false);
  const textRef = useRef<HTMLParagraphElement>(null);
  const text = messageText(row.message.content);
  const riskResults = ctx.riskResultsByMessage.get(row.message.id);
  const flagged = (riskResults?.length ?? 0) > 0;
  const sensitive = flagged && resultsAreSensitive(riskResults!);
  const { revealed, setRevealed } = useRowReveal(sensitive);
  const isUser = row.entryType === "user";
  const displayId = ctx.userLabelOverride ?? ctx.userLabel;

  useLayoutEffect(() => {
    if (expanded) return;
    const element = textRef.current;
    if (!element) return;

    const measure = () => {
      setTruncatable(element.scrollHeight > element.clientHeight + 1);
    };
    measure();

    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [expanded, text]);

  return (
    <div
      className={cn(
        "group/msg mx-3 flex items-start gap-2 px-3 py-1 hover:bg-muted/30",
        flagged &&
          "my-1 rounded-md border border-red-500 bg-red-50/50 dark:border-red-700 dark:bg-red-950/20",
      )}
    >
      {/* h-5 ≈ first-line height of text-sm/leading-relaxed; items-center
          optically centres the icon on that line without manual offsets. */}
      <div className="flex h-5 shrink-0 items-center">
        {isUser ? (
          <span className="bg-muted text-muted-foreground inline-flex size-[18px] items-center justify-center text-[9px] font-semibold">
            {userInitials(displayId)}
          </span>
        ) : (
          <Bot className="text-muted-foreground size-[18px]" />
        )}
      </div>

      {/* Message text + overlaid "show more" */}
      <div className="relative min-w-0 flex-1">
        <p
          ref={textRef}
          className={cn(
            "wrap-break-word text-sm leading-relaxed",
            isUser ? "text-foreground" : "text-muted-foreground",
            !expanded && "line-clamp-2",
            flagged && "text-red-700 dark:text-red-400",
          )}
        >
          {text}
        </p>

        {/* Gradient + "show more" — fades in on row hover when truncated */}
        {truncatable && !expanded && (
          <button
            type="button"
            className="from-background absolute right-0 bottom-0 bg-gradient-to-l from-60% to-transparent py-px pl-10 text-[11px] text-transparent opacity-0 transition-opacity group-hover/msg:text-current group-hover/msg:opacity-100 hover:underline"
            onClick={() => setExpanded(true)}
          >
            show more
          </button>
        )}

        {/* Inline accessories below expanded text */}
        {(expanded || flagged || sensitive) && (
          <div className="mt-0.5 flex items-center gap-2">
            {expanded && truncatable && (
              <button
                type="button"
                className="text-muted-foreground hover:text-foreground text-[11px] transition-colors hover:underline"
                onClick={() => setExpanded(false)}
              >
                show less
              </button>
            )}
            {flagged && <RiskBadge results={riskResults!} />}
            {sensitive && (
              <RevealSecretButton
                results={riskResults!}
                revealed={revealed}
                onToggle={() => setRevealed(!revealed)}
              />
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tool row — full ToolUI card, identical to the main transcript
// ---------------------------------------------------------------------------

function EffectsToolRow({
  row,
  ctx,
}: {
  row: ToolRow;
  ctx: EffectsViewContext;
}) {
  const summarize = useSummarizeToolCallMutation();
  const openExclusion = useContext(CreateExclusionContext);
  const { dismiss } = useDismissFinding();
  const name =
    row.toolCall?.function?.name || row.toolCall?.name || "Tool result";
  const request = argsToString(row.toolCall?.function?.arguments);
  const result = row.resultMessage
    ? messageText(row.resultMessage.content)
    : undefined;

  const callResults = row.callMessage
    ? ctx.riskResultsByMessage.get(row.callMessage.id)
    : undefined;
  const resultResults = row.resultMessage
    ? ctx.riskResultsByMessage.get(row.resultMessage.id)
    : undefined;
  const flagged =
    (callResults?.length ?? 0) > 0 || (resultResults?.length ?? 0) > 0;

  const requestRisk = toSectionRisk(
    callResults,
    request,
    "tool.args",
    openExclusion,
    dismiss,
  );
  const resultRisk = toSectionRisk(
    resultResults,
    result,
    "tool_result",
    openExclusion,
    dismiss,
  );

  const toolUseId = row.toolCall?.id ?? row.resultMessage?.toolCallId ?? "";
  const usage = ctx.claudeToolUsageByToolUseId.get(toolUseId);
  const turn = usage ? ctx.claudeTurnByPromptId.get(usage.promptId) : undefined;
  const canSummarize = Boolean(row.callMessage?.id && toolUseId);

  useEffect(() => {
    if (!row.callMessage?.id || !toolUseId) return;
    summarize.mutate({
      request: {
        summarizeToolCallRequestBody: {
          id: ctx.chatId,
          messageId: row.callMessage.id,
          toolCallId: toolUseId,
        },
      },
    });
    // One mutation instance belongs to one stable tool row. Including the
    // mutation object would restart this effect as its state changes.
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [ctx.chatId, row.callMessage?.id, toolUseId]);

  const destructive = summarize.data?.impact === "destructive";

  return (
    <div
      className={cn(
        "border-border bg-card mx-3 my-3 overflow-hidden rounded-lg border shadow-sm",
        destructive &&
          "border-yellow-400 bg-yellow-50 dark:border-yellow-700 dark:bg-yellow-950/30",
        flagged && "border-red-500 dark:border-red-700",
      )}
    >
      <div className="flex items-center gap-2 px-3 py-2">
        <Wrench className="text-muted-foreground size-3.5 shrink-0" />
        <span className="min-w-0 flex-1 truncate text-sm font-medium">
          {name}
        </span>
        {flagged && (
          <RiskBadge
            results={[...(callResults ?? []), ...(resultResults ?? [])]}
          />
        )}
        {canSummarize && summarize.isPending && (
          <Loader2
            className="text-muted-foreground size-3 animate-spin"
            aria-label="Generating summary"
          />
        )}
        {summarize.data && (
          <span
            className={cn(
              "rounded px-1.5 py-0.5 text-[10px] font-semibold tracking-wide uppercase",
              destructive
                ? "bg-yellow-200 text-yellow-900 dark:bg-yellow-900 dark:text-yellow-100"
                : "bg-muted text-muted-foreground",
            )}
          >
            {destructive ? "Destructive" : "Read only"}
          </span>
        )}
      </div>
      {summarize.data && (
        <p className="text-muted-foreground px-3 pb-2 text-sm leading-snug">
          {summarize.data.summary}
        </p>
      )}
      {summarize.isError && (
        <p className="px-3 pb-2 text-xs text-red-600">
          Could not generate a summary.
        </p>
      )}
      {request !== undefined && request !== "" && (
        <ToolUISection
          title="Arguments"
          content={request}
          defaultExpanded={Boolean(requestRisk)}
          highlight={
            requestRisk
              ? {
                  matches: requestRisk.matches,
                  masked: resultsAreSensitive(requestRisk.results),
                  headerBadge: <RiskBadge results={requestRisk.results} />,
                }
              : undefined
          }
        />
      )}
      {result !== undefined && result !== "" && (
        <ToolUISection
          title="Output"
          content={result}
          defaultExpanded={Boolean(resultRisk)}
          highlight={
            resultRisk
              ? {
                  matches: resultRisk.matches,
                  masked: resultsAreSensitive(resultRisk.results),
                  headerBadge: <RiskBadge results={resultRisk.results} />,
                }
              : undefined
          }
        />
      )}
      {(usage || turn) && (
        <div className="text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 border-t px-3 py-2 text-xs">
          {usage && (
            <span>
              Payload{" "}
              {(usage.inputSizeBytes + usage.resultSizeBytes).toLocaleString()}{" "}
              bytes
            </span>
          )}
          {turn && <span>Turn cost ${turn.costUsd.toFixed(4)}</span>}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tool group — 2+ consecutive tool rows collapsed into one ToolUIGroup
// ---------------------------------------------------------------------------

function EffectsToolGroup({
  tools,
  ctx,
}: {
  tools: ToolRow[];
  ctx: EffectsViewContext;
}) {
  const flaggedCount = tools.filter(
    (t) =>
      (t.callMessage
        ? (ctx.riskResultsByMessage.get(t.callMessage.id)?.length ?? 0)
        : 0) +
        (t.resultMessage
          ? (ctx.riskResultsByMessage.get(t.resultMessage.id)?.length ?? 0)
          : 0) >
      0,
  ).length;
  const title =
    flaggedCount > 0
      ? `Executed ${tools.length} tools · ${flaggedCount} flagged`
      : `Executed ${tools.length} tools`;

  return (
    <div className="px-3 py-3">
      <ToolUIGroup title={title} defaultExpanded={flaggedCount > 0}>
        <div className="py-1">
          {tools.map((tool) => (
            <EffectsToolRow key={tool.id} row={tool} ctx={ctx} />
          ))}
        </div>
      </ToolUIGroup>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Row dispatcher — coalesces consecutive tool rows into groups
// ---------------------------------------------------------------------------

/** Merge maximal runs of ≥2 adjacent tool rows into groups so they render as
 * a single ToolUIGroup. Single tool rows stay standalone. */
function coalesceTools(
  rows: TranscriptRow[],
): Array<MessageRow | ToolRow | ToolRow[]> {
  const out: Array<MessageRow | ToolRow | ToolRow[]> = [];
  let run: ToolRow[] = [];

  const flush = () => {
    if (run.length === 0) return;
    if (run.length === 1) out.push(run[0]!);
    else out.push(run);
    run = [];
  };

  for (const row of rows) {
    if (row.kind === "tool") {
      run.push(row);
    } else {
      flush();
      out.push(row);
    }
  }
  flush();
  return out;
}

// ---------------------------------------------------------------------------
// Public component
// ---------------------------------------------------------------------------

export function EffectsView({
  chatId,
  rows,
  riskResultsByMessage,
  claudeToolUsageByToolUseId,
  claudeTurnByPromptId,
  userLabel,
  userLabelOverride,
  isLoading,
}: {
  chatId: string;
  rows: TranscriptRow[];
  riskResultsByMessage: Map<string, RiskResult[]>;
  claudeToolUsageByToolUseId: Map<string, ClaudeToolUsage>;
  claudeTurnByPromptId: Map<string, ClaudeTurnUsage>;
  userLabel?: string;
  userLabelOverride?: string;
  isLoading?: boolean;
}): JSX.Element {
  const ctx: EffectsViewContext = useMemo(
    () => ({
      chatId,
      riskResultsByMessage,
      claudeToolUsageByToolUseId,
      claudeTurnByPromptId,
      userLabel,
      userLabelOverride,
    }),
    [
      chatId,
      riskResultsByMessage,
      claudeToolUsageByToolUseId,
      claudeTurnByPromptId,
      userLabel,
      userLabelOverride,
    ],
  );

  const items = useMemo(() => coalesceTools(rows), [rows]);

  if (isLoading) {
    return (
      <div className="text-muted-foreground flex h-full items-center justify-center text-sm">
        Loading…
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className="text-muted-foreground flex h-full items-center justify-center text-sm">
        No messages to display.
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto py-2 pb-4">
      {items.map((item) => {
        if (Array.isArray(item)) {
          return <EffectsToolGroup key={item[0]!.id} tools={item} ctx={ctx} />;
        }
        if (item.kind === "tool") {
          return <EffectsToolRow key={item.id} row={item} ctx={ctx} />;
        }
        return <CompactMessageRow key={item.id} row={item} ctx={ctx} />;
      })}
    </div>
  );
}
