import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { cn } from "@/lib/utils";
import { chatLoad } from "@gram/client/funcs/chatLoad.js";
import { SortBy, SortOrder } from "@gram/client/models/operations/listchats.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { unwrapAsync } from "@gram/client/types/fp.js";
import { useQueries } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useMemo, useState, type JSX } from "react";
import { highlight } from "./cel-highlight";
import {
  celMessageFromSample,
  sampleFromChatMessage,
  type CelSample,
} from "./cel-sample";
import { useCelEngine } from "./use-cel-engine";
import type { CelSpan } from "./cel-wasm";

const CHAT_LIMIT = 6;
const MESSAGES_PER_CHAT = 20;
const SAMPLE_CAP = 60;
const DEBOUNCE_MS = 250;

const KIND_LABELS: Record<string, string> = {
  user_message: "User",
  assistant_message: "Assistant",
  tool_request: "Tool request",
  tool_response: "Tool response",
};

const BODY_TARGETS = new Set(["content", "prompt", "assistant", "tool_result"]);

type RowState = "in" | "exempt" | "out";

type EvaluatedSample = {
  sample: CelSample;
  state: RowState;
  spans: CelSpan[];
};

function humanizeTarget(target: string): string {
  return target.replace(/^tool\./, "tool ").replace(/\./g, " ");
}

// Always-on line under the scope editors: evaluates the cumulative scope
// (include minus exempt, or a single detection expression) against recent
// real messages and reports the live count. "Show messages" opens the
// evaluated transcript sample in a side sheet.
export function CelTrafficPreview({
  includeExpr,
  exemptExpr = "",
  mode,
}: {
  includeExpr: string;
  exemptExpr?: string;
  mode: "scope" | "detection";
}): JSX.Element | null {
  const client = useGramContext();
  const engineState = useCelEngine();
  const engine = engineState.status === "ready" ? engineState.engine : null;

  const chatsQuery = useListChats(
    {
      limit: CHAT_LIMIT,
      sortBy: SortBy.LastMessageTimestamp,
      sortOrder: SortOrder.Desc,
    },
    undefined,
    { staleTime: 5 * 60 * 1000 },
  );
  const chatIds = useMemo(
    () => (chatsQuery.data?.chats ?? []).map((c) => c.id),
    [chatsQuery.data?.chats],
  );

  const messageQueries = useQueries({
    queries: chatIds.map((id) => ({
      queryKey: ["@gram/client", "chat", "traffic-sample", id],
      queryFn: ({ signal }: { signal?: AbortSignal }) =>
        unwrapAsync(
          chatLoad(client, { id, limit: MESSAGES_PER_CHAT }, undefined, {
            signal,
          }),
        ),
      staleTime: 5 * 60 * 1000,
      retry: 1,
    })),
  });
  const loading =
    chatsQuery.isLoading || messageQueries.some((q) => q.isLoading);

  // useQueries returns a fresh array of fresh results each render, so key the
  // memo on a stable fingerprint of which chats have loaded (data is immutable
  // once fetched) instead of the array identities.
  const chatData = messageQueries.map((q) => q.data);
  const loadedKey = chatData
    .map((chat, i) => (chat ? chatIds[i] : ""))
    .join("|");
  // Newest chats first, each chat's messages newest first, capped.
  const samples = useMemo(() => {
    const out: CelSample[] = [];
    for (const chat of chatData) {
      const messages = chat?.messages ?? [];
      for (let i = messages.length - 1; i >= 0; i--) {
        const sample = sampleFromChatMessage(messages[i]!);
        if (sample) out.push(sample);
        if (out.length >= SAMPLE_CAP) return out;
      }
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- loadedKey fingerprints chatData.
  }, [loadedKey]);

  const [debounced, setDebounced] = useState({
    include: includeExpr,
    exempt: exemptExpr,
  });
  useEffect(() => {
    const t = setTimeout(
      () => setDebounced({ include: includeExpr, exempt: exemptExpr }),
      DEBOUNCE_MS,
    );
    return () => clearTimeout(t);
  }, [includeExpr, exemptExpr]);

  const evaluated = useMemo<
    { kind: "ok"; rows: EvaluatedSample[] } | { kind: "invalid" } | null
  >(() => {
    if (!engine || samples.length === 0) return null;
    const include = debounced.include.trim();
    const exempt = debounced.exempt.trim();
    if (mode === "detection" && include === "") return { kind: "invalid" };
    if (include !== "" && !engine.compile(include).ok) {
      return { kind: "invalid" };
    }
    if (exempt !== "" && !engine.compile(exempt).ok) {
      return { kind: "invalid" };
    }

    const rows: EvaluatedSample[] = [];
    for (const sample of samples) {
      const message = celMessageFromSample(sample);
      let state: RowState = "in";
      let spans: CelSpan[] = [];
      if (include !== "") {
        const result = engine.evalDetection(include, message);
        if (!result.ok) continue;
        if (result.matched) {
          spans = result.spans;
        } else {
          state = "out";
        }
      }
      if (state === "in" && exempt !== "") {
        const result = engine.evalDetection(exempt, message);
        if (!result.ok) continue;
        if (result.matched) state = "exempt";
      }
      rows.push({ sample, state, spans });
    }
    return { kind: "ok", rows };
  }, [engine, samples, debounced, mode]);

  if (!engine) return null;

  return (
    <div className="flex min-h-4 flex-wrap items-center gap-x-1.5 gap-y-1 text-xs">
      <span className="text-muted-foreground">On recent traffic:</span>
      {loading && (
        <span className="text-muted-foreground flex items-center gap-1">
          <Loader2 className="h-3 w-3 animate-spin" /> sampling messages…
        </span>
      )}
      {!loading && samples.length === 0 && (
        <span className="text-muted-foreground">
          no recent messages to sample.
        </span>
      )}
      {!loading && evaluated?.kind === "invalid" && (
        <span className="text-muted-foreground">
          {mode === "detection"
            ? "enter a compiling expression to preview."
            : "fix the expressions to preview."}
        </span>
      )}
      {!loading && evaluated?.kind === "ok" && (
        <TrafficSummary mode={mode} rows={evaluated.rows} />
      )}
    </div>
  );
}

function TrafficSummary({
  mode,
  rows,
}: {
  mode: "scope" | "detection";
  rows: EvaluatedSample[];
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const inCount = rows.filter((r) => r.state === "in").length;
  const exemptCount = rows.filter((r) => r.state === "exempt").length;
  const sorted = [...rows].sort((a, b) => rowRank(a.state) - rowRank(b.state));

  return (
    <>
      <span className="text-foreground">
        {mode === "scope" ? (
          <>
            <span className="font-medium">{inCount}</span> of {rows.length}{" "}
            messages in scope
            {exemptCount > 0 && <> ({exemptCount} exempted)</>}
          </>
        ) : (
          <>
            <span className="font-medium">{inCount}</span> of {rows.length}{" "}
            messages match
          </>
        )}
      </span>
      <Sheet open={open} onOpenChange={setOpen}>
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="text-muted-foreground hover:text-foreground underline underline-offset-2"
        >
          Show messages
        </button>
        <SheetContent className="flex w-full flex-col sm:max-w-2xl">
          <SheetHeader>
            <SheetTitle>Recent traffic</SheetTitle>
            <SheetDescription>
              {mode === "scope"
                ? `The include and exempt expressions evaluated together: ${inCount} of ${rows.length} sampled messages in scope${exemptCount > 0 ? `, ${exemptCount} exempted` : ""}.`
                : `${inCount} of ${rows.length} sampled messages match the expression.`}
            </SheetDescription>
          </SheetHeader>
          <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-6">
            <ul className="border-border divide-border divide-y rounded-md border">
              {sorted.map((row, i) => (
                <TrafficRow key={i} row={row} mode={mode} />
              ))}
            </ul>
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}

function rowRank(state: RowState): number {
  switch (state) {
    case "in":
      return 0;
    case "exempt":
      return 1;
    case "out":
      return 2;
  }
}

const COLLAPSED_SPAN_PILLS = 4;

function TrafficRow({
  row,
  mode,
}: {
  row: EvaluatedSample;
  mode: "scope" | "detection";
}): JSX.Element {
  const [expanded, setExpanded] = useState(false);
  const { sample, state, spans } = row;
  const bodySpans = spans.filter((s) => BODY_TARGETS.has(s.Target));
  const preview =
    sample.kind === "tool_request"
      ? sample.tools.map((t) => `${t.name} ${t.args}`.trim()).join(" · ")
      : sample.content;
  const visibleSpans = expanded ? spans : spans.slice(0, COLLAPSED_SPAN_PILLS);

  return (
    <li>
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
        className={cn(
          "flex w-full items-start gap-2 px-2.5 py-2 text-left transition-colors",
          state === "in"
            ? "bg-success/10 hover:bg-success/15"
            : "hover:bg-muted/40",
          state === "out" && "opacity-60",
        )}
      >
        <span
          className={cn(
            "mt-1 size-1.5 shrink-0 rounded-full",
            state === "in" && "bg-success",
            state === "exempt" && "bg-warning",
            state === "out" && "bg-border",
          )}
          aria-label={rowStateLabel(state, mode)}
        />
        <span className="text-muted-foreground w-24 shrink-0 text-xs">
          {KIND_LABELS[sample.kind] ?? sample.kind}
        </span>
        <span className="min-w-0 flex-1 space-y-1">
          <span
            className={cn(
              "text-foreground block text-xs break-all whitespace-pre-wrap",
              !expanded && "line-clamp-2",
            )}
          >
            {sample.kind === "tool_request" && !expanded ? preview : null}
            {sample.kind !== "tool_request"
              ? highlight(preview, bodySpans)
              : null}
            {sample.kind === "tool_request" && expanded && (
              <span className="flex flex-col gap-1">
                {sample.content && <span>{sample.content}</span>}
                {sample.tools.map((t, i) => (
                  <code key={i} className="font-mono">
                    {t.name} {t.args}
                  </code>
                ))}
              </span>
            )}
          </span>
          {state === "in" && spans.length > 0 && (
            <span className="flex flex-wrap gap-1">
              {visibleSpans.map((s, i) => (
                <span
                  key={`${s.Target}-${s.Start}-${i}`}
                  className="border-success/40 bg-success/10 text-foreground inline-flex max-w-full items-center rounded-full border px-1.5 py-0.5 text-[11px]"
                >
                  <span className="text-muted-foreground mr-1">
                    {humanizeTarget(s.Target)}
                    {s.Path ? `.${s.Path}` : ""}:
                  </span>
                  <code className="truncate font-mono">{s.Value}</code>
                </span>
              ))}
              {spans.length > visibleSpans.length && (
                <span className="text-muted-foreground text-[11px]">
                  +{spans.length - visibleSpans.length} more
                </span>
              )}
            </span>
          )}
        </span>
        {state === "exempt" && (
          <span className="text-warning shrink-0 text-xs">exempted</span>
        )}
      </button>
    </li>
  );
}

function rowStateLabel(state: RowState, mode: "scope" | "detection"): string {
  if (mode === "detection") return state === "in" ? "matches" : "no match";
  switch (state) {
    case "in":
      return "in scope";
    case "exempt":
      return "exempted";
    case "out":
      return "out of scope";
  }
}
