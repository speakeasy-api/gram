import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import {
  getMatchStrings,
  jsonEscaped,
  matchRanges,
  needsWholeMessageMask,
  resultIsSpanlessSensitive,
  withJsonEscaped,
} from "@/pages/chatLogs/chatHelpers";
import { argsToString, messageText } from "@/pages/chatLogs/transcript";
import {
  getTraceEntryType,
  parseToolCalls,
} from "@/pages/chatLogs/traceEntries";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useLoadChat } from "@gram/client/react-query/loadChat.js";
import { useEffect, useMemo, type ReactNode } from "react";
import {
  DRAWER_CELL_LABEL,
  DRAWER_FOOTNOTE,
  DrawerSection,
  NoRevealAccessNote,
  RevealFailedNote,
  RevealingNote,
  RevealToggleButton,
} from "./finding-drawer-parts";
import type { FindingKind } from "./finding-kind";
import { redactionChipLabel } from "./payload-spans";
import { RedactionChip, RevealedSpan } from "./risk-ui";
import { SEVERITY_EDGE, SEVERITY_TEXT } from "./risk-severity";
import type { SeverityRating } from "./risk-utils";
import { TRANSCRIPT_DENIED_REASON, useUnmaskedMatch } from "./unmask";

// Same initial window the transcript sheet loads around risk findings.
const CONTEXT_WINDOW_LIMIT = 200;

type ExcerptMessage = {
  id: string;
  role: string;
  mono: boolean;
  text: string;
};

function excerptMessage(message: ChatMessage): ExcerptMessage {
  const calls = parseToolCalls(message.toolCalls);
  const body = messageText(message.content);
  switch (getTraceEntryType(message, calls)) {
    case "tool_call": {
      const names = (calls ?? [])
        .map((c) => c.function?.name ?? c.name)
        .filter(Boolean)
        .join(", ");
      const args = (calls ?? [])
        .map((c) => argsToString(c.function?.arguments))
        .filter(Boolean);
      return {
        id: message.id,
        role: names ? `Tool request · ${names}` : "Tool request",
        mono: true,
        text: [body, ...args].filter(Boolean).join("\n"),
      };
    }
    case "tool_result":
      return { id: message.id, role: "Tool response", mono: true, text: body };
    case "user":
      return { id: message.id, role: "User prompt", mono: false, text: body };
    case "assistant":
      return { id: message.id, role: "Assistant", mono: false, text: body };
    case "system":
      return { id: message.id, role: "System", mono: false, text: body };
  }
}

/** Splits `text` at every masked match into plain and flagged pieces. */
function MaskedText({
  text,
  matches,
  currentMatch,
  revealed,
  fingerprintForMatch,
}: {
  text: string;
  matches: string[];
  currentMatch: string | undefined;
  revealed: boolean;
  fingerprintForMatch: (value: string) => string | undefined;
}): ReactNode {
  const ranges = matchRanges(text, matches);
  if (ranges.length === 0) return text;
  const nodes: ReactNode[] = [];
  let pos = 0;
  ranges.forEach(([start, end], k) => {
    if (start > pos) nodes.push(text.slice(pos, start));
    const value = text.slice(start, end);
    const selected =
      currentMatch !== undefined &&
      (value === currentMatch || value === jsonEscaped(currentMatch));
    nodes.push(
      revealed ? (
        <RevealedSpan key={k} selected={selected}>
          {value}
        </RevealedSpan>
      ) : (
        <RedactionChip
          key={k}
          label={redactionChipLabel(value.length)}
          title={fingerprintForMatch(value)}
          selected={selected}
        />
      ),
    );
    pos = end;
  });
  if (pos < text.length) nodes.push(text.slice(pos));
  return nodes;
}

function ExcerptRow({
  message,
  flagged,
  first,
  rating,
  children,
}: {
  message: ExcerptMessage;
  flagged: boolean;
  first: boolean;
  rating: SeverityRating | null;
  children: ReactNode;
}): JSX.Element {
  return (
    <div
      className={cn(
        "grid grid-cols-[120px_minmax(0,1fr)] gap-3.5 border-l-2 py-3 pr-3.5 pl-3",
        !first && "border-t-muted border-t",
        flagged
          ? cn(
              "bg-[var(--color-feedback-orange-400)]/6",
              rating ? SEVERITY_EDGE[rating] : "border-l-foreground",
            )
          : "border-l-transparent",
      )}
    >
      <div className="flex min-w-0 flex-col gap-1">
        <span className={DRAWER_CELL_LABEL}>{message.role}</span>
        {flagged && (
          <span
            className={cn(
              "font-mono text-[10px] tracking-[0.1em] uppercase",
              rating ? SEVERITY_TEXT[rating] : "text-foreground",
            )}
          >
            Flagged
          </span>
        )}
      </div>
      <div
        className={cn(
          "min-w-0 leading-[1.6] break-words whitespace-pre-wrap",
          message.mono ? "font-mono text-xs" : "text-sm",
          flagged ? "text-foreground" : "text-muted-foreground",
        )}
      >
        {children}
      </div>
    </div>
  );
}

// Position of the flagged message is only knowable when the risk window
// starts at the top of the chat with no gap before it.
function messagePosition(
  data: { riskSegments?: { hasMoreBefore: boolean; lastSeq: number }[] },
  messages: ChatMessage[],
  idx: number,
): number | null {
  const first = data.riskSegments?.[0];
  const flagged = messages[idx];
  if (!first || !flagged || first.hasMoreBefore) return null;
  return flagged.seq <= first.lastSeq ? idx + 1 : null;
}

export function FindingContext({
  result,
  kind,
  chatResults,
  chatResultsComplete,
  revealed,
  canReveal,
  rating,
  onToggleReveal,
  onRequestReveal,
}: {
  result: RiskResult;
  kind: FindingKind;
  /** This chat's findings, carrying the raw matches the excerpt masks.
   * Undefined while loading, so no message renders before it can be masked. */
  chatResults: RiskResult[] | undefined;
  /** `chatResults` holds every finding in the chat. */
  chatResultsComplete: boolean;
  revealed: boolean;
  canReveal: boolean;
  rating: SeverityRating | null;
  onToggleReveal: () => void;
  onRequestReveal: () => void;
}): JSX.Element {
  const chatId = result.chatId ?? "";
  const chatQuery = useLoadChat(
    { id: chatId, limit: CONTEXT_WINDOW_LIMIT, riskOnly: true },
    undefined,
    { enabled: Boolean(chatId), throwOnError: false },
  );
  // Each audited reveal is recorded against this finding; the transcript text
  // itself already follows transcript access.
  const unmask = useUnmaskedMatch(result.id);
  const auditedReveal = kind !== "analyzer";
  const { reveal, isError: revealFailed } = unmask;
  // A failed reveal waits for Retry rather than looping.
  useEffect(() => {
    if (revealed && canReveal && auditedReveal && !revealFailed) reveal();
  }, [revealed, canReveal, auditedReveal, revealFailed, reveal]);

  const shown =
    revealed &&
    canReveal &&
    (!auditedReveal || unmask.value !== null || unmask.evidenceNotStored);

  const matches = useMemo(
    () => withJsonEscaped(getMatchStrings(chatResults)),
    [chatResults],
  );
  const currentMatch = chatResults?.find((r) => r.id === result.id)?.match;
  const fingerprintForMatch = (value: string) =>
    chatResults?.find(
      (r) => r.match === value || (r.match && jsonEscaped(r.match) === value),
    )?.matchRedacted;

  const messages = chatQuery.data?.messages ?? [];
  const idx = messages.findIndex((m) => m.id === result.chatMessageId);
  const excerpt = idx >= 0 ? messages.slice(Math.max(0, idx - 2), idx + 2) : [];
  const position =
    chatQuery.data && idx >= 0
      ? messagePosition(chatQuery.data, messages, idx)
      : null;
  const total = chatQuery.data?.numMessages;

  const resultIsWhole =
    kind === "judge" ||
    (kind === "analyzer" && resultIsSpanlessSensitive(result));
  // Spanless findings, or matches that could not all be loaded, leave only
  // the whole message to mask.
  const masksWholeMessage = (messageId: string): boolean =>
    !chatResultsComplete ||
    (resultIsWhole && messageId === result.chatMessageId) ||
    needsWholeMessageMask(
      chatResults?.filter((r) => r.chatMessageId === messageId),
    );
  const maskable =
    kind === "chat" ||
    resultIsWhole ||
    excerpt.some((m) => masksWholeMessage(m.id));

  const forbidden =
    chatQuery.error instanceof GramError && chatQuery.error.statusCode === 403;

  let body: ReactNode;
  if (!chatId) {
    body = (
      <p className="text-muted-foreground text-sm">
        This finding is not linked to a session.
      </p>
    );
  } else if (chatQuery.isLoading || chatResults === undefined) {
    body = (
      <div className="bg-card border p-4">
        <Skeleton>
          <div className="h-4 w-3/4" />
          <div className="h-4 w-full" />
          <div className="h-4 w-1/2" />
        </Skeleton>
      </div>
    );
  } else if (forbidden) {
    body = (
      <p className="text-muted-foreground text-sm">
        {TRANSCRIPT_DENIED_REASON}
      </p>
    );
  } else if (chatQuery.isError) {
    body = (
      <div className="flex items-center gap-2">
        <span className="text-muted-foreground text-sm">
          Failed to load the transcript.
        </span>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => void chatQuery.refetch()}
        >
          <Button.Text>Retry</Button.Text>
        </Button>
      </div>
    );
  } else if (excerpt.length === 0) {
    body = (
      <p className="text-muted-foreground text-sm">
        The flagged message is outside the loaded transcript. Open the
        transcript to find it.
      </p>
    );
  } else {
    body = (
      <div className="bg-card border">
        <div className="border-b-muted flex items-center justify-between gap-3 border-b px-3.5 py-2.5">
          <span className="truncate text-[13px] font-normal">
            {result.chatTitle ?? "Untitled"}
          </span>
          <span className="text-muted-foreground font-mono text-[11px] whitespace-nowrap">
            {position != null && total != null
              ? `Message ${position} of ${total}`
              : `${total ?? messages.length} messages`}
          </span>
        </div>
        {excerpt.map((raw, i) => {
          const message = excerptMessage(raw);
          const flagged = raw.id === result.chatMessageId;
          return (
            <ExcerptRow
              key={raw.id}
              message={message}
              flagged={flagged}
              first={i === 0}
              rating={rating}
            >
              {masksWholeMessage(raw.id) && !shown ? (
                <RedactionChip
                  label={`${flagged ? "Flagged event" : "Masked message"} · ${message.text.length.toLocaleString()} chars${canReveal ? " · reveal" : ""}`}
                  locked={!canReveal}
                  onClick={canReveal ? onRequestReveal : undefined}
                />
              ) : (
                <MaskedText
                  text={message.text}
                  matches={kind === "chat" || !flagged ? matches : []}
                  currentMatch={currentMatch}
                  revealed={shown}
                  fingerprintForMatch={fingerprintForMatch}
                />
              )}
            </ExcerptRow>
          );
        })}
      </div>
    );
  }

  return (
    <DrawerSection
      label="In context"
      aside={
        maskable && canReveal ? (
          <RevealToggleButton
            revealed={revealed}
            onToggle={onToggleReveal}
            revealLabel={kind === "judge" ? "Reveal event" : "Reveal matches"}
          />
        ) : null
      }
    >
      {body}
      {revealed && canReveal && auditedReveal && unmask.isLoading && (
        <RevealingNote className="text-muted-foreground" />
      )}
      {revealed && canReveal && auditedReveal && unmask.isError && (
        <RevealFailedNote onRetry={reveal} />
      )}
      {maskable && !canReveal && <NoRevealAccessNote />}
      {maskable && (
        <span className={DRAWER_FOOTNOTE}>
          Reveals are audited · Surrounding messages follow transcript access
        </span>
      )}
    </DrawerSection>
  );
}
