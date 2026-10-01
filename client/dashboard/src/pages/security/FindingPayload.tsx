import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { addDays } from "date-fns";
import { useCallback, useMemo } from "react";
import {
  DRAWER_FOOTNOTE,
  DrawerSection,
  NoRevealAccessNote,
  RevealFailedNote,
  RevealingNote,
  RevealToggleButton,
} from "./finding-drawer-parts";
import {
  buildSpanRanges,
  redactionChipLabel,
  findingByteSpans,
  layoutPayload,
  type PayloadSegment,
  type PayloadTokenKind,
  type SpanRange,
} from "./payload-spans";
import { RevealAllContext } from "./reveal-all-context";
import type { ExecutionPayload } from "./use-execution-payload";
import { MaskedMatch, RedactionChip, RevealedSpan } from "./risk-ui";

// Mirrors mcpFindingEvidenceRetention on the server.
const EVIDENCE_RETENTION_DAYS = 90;

const TOKEN_CLASS: Record<PayloadTokenKind, string> = {
  key: "text-[var(--ch-5)]",
  str: "text-[var(--ch-4)]",
  num: "text-[var(--ch-7)]",
  punc: "text-[var(--ch-1)]",
  text: "text-[var(--ch-4)]",
};

function PayloadSegmentView({
  seg,
  range,
  currentId,
  revealed,
  fingerprintFor,
  onSelect,
}: {
  seg: PayloadSegment;
  range: SpanRange | undefined;
  currentId: string;
  revealed: boolean;
  fingerprintFor: (id: string) => string | undefined;
  onSelect: (id: string) => void;
}): JSX.Element | null {
  if (!range) {
    return <span className={TOKEN_CLASS[seg.kind]}>{seg.text}</span>;
  }
  const selected = range.ids.includes(currentId);
  const target = selected ? currentId : range.ids[0]!;
  if (revealed) {
    return (
      <RevealedSpan selected={selected} onClick={() => onSelect(target)}>
        <span className="text-[var(--ch-10)]">{seg.text}</span>
      </RevealedSpan>
    );
  }
  // A masked range renders one chip where it opens and hides the rest.
  if (!seg.rangeStart) return null;
  return (
    <RedactionChip
      label={redactionChipLabel(range.end - range.start)}
      title={fingerprintFor(target)}
      selected={selected}
      onClick={() => onSelect(target)}
    />
  );
}

function PayloadCode({
  payload,
  result,
  siblings,
  revealed,
  onSelect,
}: {
  payload: string;
  result: RiskResult;
  siblings: RiskResult[];
  revealed: boolean;
  onSelect: (id: string) => void;
}): JSX.Element {
  const samePhase = useMemo(
    () =>
      [result, ...siblings.filter((s) => s.id !== result.id)].filter(
        (s) => !s.phase || !result.phase || s.phase === result.phase,
      ),
    [result, siblings],
  );
  const ranges = useMemo(
    () => buildSpanRanges(payload, samePhase.flatMap(findingByteSpans)),
    [payload, samePhase],
  );
  const lines = useMemo(
    () => layoutPayload(payload, ranges),
    [payload, ranges],
  );
  const fingerprintFor = useCallback(
    (id: string) => samePhase.find((s) => s.id === id)?.matchRedacted,
    [samePhase],
  );

  return (
    <div className="bg-foreground dark:bg-card overflow-x-auto py-3.5 font-mono text-xs leading-[1.7]">
      {/* Scopes the code-syntax palette to its dark values on this always-dark
          block. */}
      <div className="dark">
        {lines.map((line, i) => (
          <div key={i} className="grid grid-cols-[40px_minmax(0,1fr)] pr-4">
            <span className="pr-3.5 text-right text-[var(--ch-1)] opacity-70 select-none">
              {i + 1}
            </span>
            <span className="break-all whitespace-pre-wrap">
              {line.length === 0
                ? " "
                : line.map((seg, k) => (
                    <PayloadSegmentView
                      key={k}
                      seg={seg}
                      range={
                        seg.range === undefined ? undefined : ranges[seg.range]
                      }
                      currentId={result.id}
                      revealed={revealed}
                      fingerprintFor={fingerprintFor}
                      onSelect={onSelect}
                    />
                  ))}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

// The single-match view used before the payload is revealed, and whenever it
// can't be. Isolated from the page's reveal-all so it never fires its own
// unmask behind the drawer's payload reveal.
function MatchFallback({ result }: { result: RiskResult }): JSX.Element {
  return (
    <div className="bg-foreground px-4 py-3.5">
      <RevealAllContext.Provider value={null}>
        <MaskedMatch
          tone="contrast"
          wrap
          resultId={result.id}
          matchRedacted={result.matchRedacted}
        />
      </RevealAllContext.Provider>
    </div>
  );
}

function EvidenceNotStoredBlock({
  fingerprint,
}: {
  fingerprint: string | undefined;
}): JSX.Element {
  return (
    <div className="bg-card flex flex-col gap-2.5 border p-5">
      <span className="text-sm font-normal">Evidence not stored</span>
      <p className="text-muted-foreground text-[13px] text-pretty">
        This finding was recorded before MCP evidence storage, or its 90-day
        retention window has passed. Only the redacted fingerprint remains.
      </p>
      {fingerprint && (
        <span className="self-start border px-1.5 py-0.5 font-mono text-xs">
          {fingerprint}
        </span>
      )}
    </div>
  );
}

function PayloadBody({
  result,
  siblings,
  revealed,
  canReveal,
  payload,
  onSelect,
}: {
  result: RiskResult;
  siblings: RiskResult[];
  revealed: boolean;
  canReveal: boolean;
  payload: ExecutionPayload;
  onSelect: (id: string) => void;
}): JSX.Element {
  if (!canReveal) return <MatchFallback result={result} />;
  if (payload.data?.revealState === "evidence_not_stored") {
    return <EvidenceNotStoredBlock fingerprint={result.matchRedacted} />;
  }
  if (payload.data) {
    return (
      <PayloadCode
        payload={payload.data.payload}
        result={result}
        siblings={siblings}
        revealed={revealed}
        onSelect={onSelect}
      />
    );
  }
  if (payload.isPending) {
    return (
      <div className="bg-foreground text-background/70 px-4 py-3.5">
        <RevealingNote />
      </div>
    );
  }
  if (payload.isError) return <RevealFailedNote onRetry={payload.reveal} />;
  return <MatchFallback result={result} />;
}

export function FindingPayload({
  result,
  siblings,
  revealed,
  canReveal,
  payload,
  onToggleReveal,
  onSelect,
}: {
  result: RiskResult;
  siblings: RiskResult[];
  revealed: boolean;
  canReveal: boolean;
  payload: ExecutionPayload;
  onToggleReveal: () => void;
  onSelect: (id: string) => void;
}): JSX.Element {
  const notStored = payload.data?.revealState === "evidence_not_stored";
  const expiresAt =
    payload.data?.expiresAt ??
    addDays(result.createdAt, EVIDENCE_RETENTION_DAYS);
  return (
    <DrawerSection
      label={
        result.phase === "response"
          ? "Response · tool result"
          : "Request · tools/call params"
      }
      aside={
        canReveal && !notStored ? (
          <RevealToggleButton revealed={revealed} onToggle={onToggleReveal} />
        ) : null
      }
    >
      <PayloadBody
        result={result}
        siblings={siblings}
        revealed={revealed}
        canReveal={canReveal}
        payload={payload}
        onSelect={onSelect}
      />
      {!canReveal && <NoRevealAccessNote />}
      {canReveal && !notStored && (
        <span className={DRAWER_FOOTNOTE}>
          Reveals are audited · Evidence retained until{" "}
          {expiresAt.toLocaleDateString()}
        </span>
      )}
    </DrawerSection>
  );
}
