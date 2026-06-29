import { useCallback, useEffect, useRef, useState } from "react";
import type {
  Chat,
  ChatMessage,
  RiskSegment,
} from "@gram/client/models/components";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useLoadChat } from "@gram/client/react-query";
import { useSdkClient } from "@/contexts/Sdk";
import { TRANSCRIPT_PAGE_SIZE } from "./useChatTranscript";

// Initial window is generous so most matches + context arrive in one request;
// gaps between disjoint windows are expanded on demand.
const WINDOW_INITIAL_LIMIT = 200;

type WindowLoadKey = "before" | "after" | `gap:${number}`;

export interface WindowedTranscript {
  chat: Chat | undefined;
  messages: ChatMessage[];
  /** seqs after which an un-loaded gap remains, between disjoint windows. */
  gaps: Set<number>;
  hasMoreBefore: boolean;
  hasMoreAfter: boolean;
  loadBefore: () => void;
  loadAfter: () => void;
  loadGap: (afterSeq: number) => void;
  loadingKey: WindowLoadKey | null;
  isLoading: boolean;
  isError: boolean;
}

interface WindowState {
  messages: ChatMessage[];
  gaps: Set<number>;
  hasMoreBefore: boolean;
  hasMoreAfter: boolean;
}

/** The initial windowed request (everything but the chat id). Exactly one mode
 * is used: `{ riskOnly: true }` windows around risk findings, `{ query }`
 * windows around text-search matches. */
export interface WindowedRequest {
  riskOnly?: boolean;
  query?: string;
}

// mergeSorted unions two message lists, dedupes by seq, and keeps ascending order.
function mergeSorted(a: ChatMessage[], b: ChatMessage[]): ChatMessage[] {
  const bySeq = new Map<number, ChatMessage>();
  for (const m of a) bySeq.set(m.seq, m);
  for (const m of b) bySeq.set(m.seq, m);
  return Array.from(bySeq.values()).sort((x, y) => x.seq - y.seq);
}

// buildInitial seeds state from a windowed response: the flat windowed message
// list plus one gap marker per boundary between disjoint segments.
function buildInitial(chat: Chat, segments: RiskSegment[]): WindowState {
  const gaps = new Set<number>();
  for (let i = 0; i < segments.length - 1; i++) {
    gaps.add(segments[i]!.lastSeq);
  }
  return {
    messages: chat.messages,
    gaps,
    hasMoreBefore: segments[0]?.hasMoreBefore ?? false,
    hasMoreAfter: segments[segments.length - 1]?.hasMoreAfter ?? false,
  };
}

// useWindowedTranscript loads a windowed slice of a chat plus surrounding
// context, then lets the user expand the edges and fill the gaps between
// disjoint windows. The mode is decided by `request`: `{ riskOnly: true }`
// windows around risk findings (segments in `risk_segments`), `{ query }`
// windows around text-search matches (segments in `match_segments`). The
// incremental expansion is identical for both modes: plain before_seq/after_seq
// page loads merged into the window.
export function useWindowedTranscript(
  chatId: string,
  enabled: boolean,
  request: WindowedRequest,
): WindowedTranscript {
  const client = useSdkClient();
  // Which response field carries the window segments depends on the mode. A
  // primitive (not a derived array) so it's a stable effect dependency.
  const riskOnly = request.riskOnly ?? false;
  const base = useLoadChat(
    { id: chatId, limit: WINDOW_INITIAL_LIMIT, ...request },
    undefined,
    {
      enabled,
      throwOnError: (error) =>
        !(error instanceof GramError && error.statusCode === 404),
    },
  );

  const [state, setState] = useState<WindowState | null>(null);
  const [loadingKey, setLoadingKey] = useState<WindowLoadKey | null>(null);
  const [loadError, setLoadError] = useState(false);

  // A stable key for the active windowed request (chat + mode). A slow in-flight
  // page from a previous chat OR a previous query/risk request must not apply its
  // response into the now-current transcript, so each load captures this key and
  // drops its result/error if the key changed mid-flight.
  const requestKey = `${chatId}|${JSON.stringify(request)}`;
  const requestKeyRef = useRef(requestKey);
  requestKeyRef.current = requestKey;

  // Clear in-flight loading state when the request changes (chat switch or new
  // query). The previous request's in-flight load is guarded out of its own
  // finally (it checks requestKeyRef), so without this reset `loadingKey` could
  // stay stuck and permanently disable loadBefore/After/Gap.
  useEffect(() => {
    setLoadingKey(null);
    setLoadError(false);
  }, [requestKey]);

  // Re-seed whenever a fresh base response arrives (chat switch, query change,
  // or refetch). User expansions and any prior incremental-load error are reset.
  useEffect(() => {
    if (base.data) {
      const segments =
        (riskOnly ? base.data.riskSegments : base.data.matchSegments) ?? [];
      setState(buildInitial(base.data, segments));
      setLoadError(false);
    }
  }, [base.data, riskOnly]);

  // Runs one incremental page load: ignores the result if the active request
  // changed mid-flight (chat switch or new query), and records failures so they
  // surface via isError instead of silently no-opping.
  const runIncrementalLoad = useCallback(
    (
      key: WindowLoadKey,
      req: { beforeSeq?: number; afterSeq?: number },
      apply: (prev: WindowState, page: Chat) => WindowState,
    ) => {
      if (loadingKey) return;
      const startKey = requestKeyRef.current;
      setLoadingKey(key);
      void client.chat
        .load({ id: chatId, limit: TRANSCRIPT_PAGE_SIZE, ...req })
        .then((page) => {
          if (requestKeyRef.current !== startKey) return; // request changed
          setState((prev) => (prev ? apply(prev, page) : prev));
        })
        .catch(() => {
          if (requestKeyRef.current === startKey) setLoadError(true);
        })
        .finally(() => {
          if (requestKeyRef.current === startKey) setLoadingKey(null);
        });
    },
    [client, chatId, loadingKey],
  );

  const loadBefore = useCallback(() => {
    if (!state || !state.hasMoreBefore) return;
    const oldest = state.messages[0];
    if (!oldest) return;
    runIncrementalLoad("before", { beforeSeq: oldest.seq }, (prev, page) => ({
      ...prev,
      messages: mergeSorted(page.messages, prev.messages),
      hasMoreBefore: page.hasMoreBefore,
    }));
  }, [state, runIncrementalLoad]);

  const loadAfter = useCallback(() => {
    if (!state || !state.hasMoreAfter) return;
    const newest = state.messages[state.messages.length - 1];
    if (!newest) return;
    runIncrementalLoad("after", { afterSeq: newest.seq }, (prev, page) => ({
      ...prev,
      messages: mergeSorted(prev.messages, page.messages),
      hasMoreAfter: page.hasMoreAfter,
    }));
  }, [state, runIncrementalLoad]);

  const loadGap = useCallback(
    (afterSeq: number) => {
      if (!state) return;
      runIncrementalLoad(`gap:${afterSeq}`, { afterSeq }, (prev, page) => {
        // The message immediately after the gap, by seq (messages stay sorted
        // ascending via mergeSorted, so find() is enough).
        const nextSeq = prev.messages.find((m) => m.seq > afterSeq)?.seq;
        const maxLoaded = page.messages.reduce(
          (mx, m) => Math.max(mx, m.seq),
          afterSeq,
        );
        const gaps = new Set(prev.gaps);
        gaps.delete(afterSeq);
        // Gap stays open (advanced to the new edge) only if the page didn't
        // reach the next already-loaded message.
        if (nextSeq !== undefined && maxLoaded < nextSeq) {
          gaps.add(maxLoaded);
        }
        return {
          ...prev,
          messages: mergeSorted(prev.messages, page.messages),
          gaps,
        };
      });
    },
    [state, runIncrementalLoad],
  );

  return {
    chat: base.data,
    messages: state?.messages ?? [],
    gaps: state?.gaps ?? new Set(),
    hasMoreBefore: state?.hasMoreBefore ?? false,
    hasMoreAfter: state?.hasMoreAfter ?? false,
    loadBefore,
    loadAfter,
    loadGap,
    loadingKey,
    isLoading: base.isLoading,
    isError: base.isError || loadError,
  };
}
