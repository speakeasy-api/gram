import { useCallback, useEffect, useRef, useState } from "react";
import type { Chat } from "@gram/client/models/components/chat.js";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import type { RiskSegment } from "@gram/client/models/components/risksegment.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useLoadChat } from "@gram/client/react-query/loadChat.js";
import { useSdkClient } from "@/contexts/Sdk";
import { TRANSCRIPT_PAGE_SIZE } from "./useChatTranscript";
import { fetchFullTranscript, FULL_LOAD_PAGE_SIZE } from "./transcriptFull";

// Initial window is generous so most matches + context arrive in one request;
// gaps between disjoint windows are expanded on demand.
const WINDOW_INITIAL_LIMIT = 200;

type WindowLoadKey = "before" | "after" | "all" | `gap:${number}`;

export interface WindowedTranscript {
  chat: Chat | undefined;
  messages: ChatMessage[];
  /** seqs after which an un-loaded gap remains, between disjoint windows. */
  gaps: Set<number>;
  hasMoreBefore: boolean;
  hasMoreAfter: boolean;
  /** Single-page edge loads, for scroll-driven streaming. */
  loadBefore: () => void;
  loadAfter: () => void;
  /** Full-range loads: everything above / below the window, the entirety of
   * one gap, or the chat's whole remaining history. */
  loadAllBefore: () => void;
  loadAllAfter: () => void;
  loadGap: (afterSeq: number) => void;
  loadAll: () => void;
  loadingKey: WindowLoadKey | null;
  isLoading: boolean;
  isError: boolean;
  /** True when the load failed specifically because the caller lacks
   * chat:read for this chat, so callers can show a permission message
   * instead of a generic error. */
  isForbidden: boolean;
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
// incremental expansion is identical for both modes: scrolling streams
// single pages at the edges, while the divider buttons load a full range
// (the whole gap / everything above or below) in one action.
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
      // Let 404s and 403s fall through to the panel's "Not found" /
      // "Permission denied" UI instead of throwing to the nearest error
      // boundary — both are anticipated outcomes of opening an arbitrary
      // chat_id, not unexpected failures.
      throwOnError: (error) =>
        !(
          error instanceof GramError &&
          (error.statusCode === 404 || error.statusCode === 403)
        ),
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
  // stay stuck and permanently disable the incremental loads.
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

  // Runs one incremental load: `run` fetches (one page or a whole range) and
  // returns the state update to apply. The result is ignored if the active
  // request changed mid-flight (chat switch or new query), and failures are
  // recorded so they surface via isError instead of silently no-opping.
  const runIncrementalLoad = useCallback(
    (
      key: WindowLoadKey,
      run: () => Promise<(prev: WindowState) => WindowState>,
    ) => {
      if (loadingKey) return;
      const startKey = requestKeyRef.current;
      setLoadingKey(key);
      void run()
        .then((apply) => {
          if (requestKeyRef.current !== startKey) return; // request changed
          setState((prev) => (prev ? apply(prev) : prev));
        })
        .catch(() => {
          if (requestKeyRef.current === startKey) setLoadError(true);
        })
        .finally(() => {
          if (requestKeyRef.current === startKey) setLoadingKey(null);
        });
    },
    [loadingKey],
  );

  const loadBefore = useCallback(() => {
    if (!state || !state.hasMoreBefore) return;
    const oldest = state.messages[0];
    if (!oldest) return;
    runIncrementalLoad("before", async () => {
      const page = await client.chat.load({
        id: chatId,
        beforeSeq: oldest.seq,
        limit: TRANSCRIPT_PAGE_SIZE,
      });
      return (prev) => ({
        ...prev,
        messages: mergeSorted(page.messages, prev.messages),
        hasMoreBefore: page.hasMoreBefore,
      });
    });
  }, [state, runIncrementalLoad, client, chatId]);

  const loadAfter = useCallback(() => {
    if (!state || !state.hasMoreAfter) return;
    const newest = state.messages[state.messages.length - 1];
    if (!newest) return;
    runIncrementalLoad("after", async () => {
      const page = await client.chat.load({
        id: chatId,
        afterSeq: newest.seq,
        limit: TRANSCRIPT_PAGE_SIZE,
      });
      return (prev) => ({
        ...prev,
        messages: mergeSorted(prev.messages, page.messages),
        hasMoreAfter: page.hasMoreAfter,
      });
    });
  }, [state, runIncrementalLoad, client, chatId]);

  const loadAllBefore = useCallback(() => {
    if (!state || !state.hasMoreBefore) return;
    const oldest = state.messages[0];
    if (!oldest) return;
    runIncrementalLoad("before", async () => {
      const collected: ChatMessage[] = [];
      let cursor = oldest.seq;
      let more = true;
      while (more) {
        const page = await client.chat.load({
          id: chatId,
          beforeSeq: cursor,
          limit: FULL_LOAD_PAGE_SIZE,
        });
        collected.push(...page.messages);
        // Pages arrive ascending; the first row is the new oldest edge.
        const pageOldest = page.messages[0];
        more = page.hasMoreBefore && !!pageOldest && pageOldest.seq !== cursor;
        cursor = pageOldest?.seq ?? cursor;
      }
      return (prev) => ({
        ...prev,
        messages: mergeSorted(collected, prev.messages),
        hasMoreBefore: false,
      });
    });
  }, [state, runIncrementalLoad, client, chatId]);

  const loadAllAfter = useCallback(() => {
    if (!state || !state.hasMoreAfter) return;
    const newest = state.messages[state.messages.length - 1];
    if (!newest) return;
    runIncrementalLoad("after", async () => {
      const collected: ChatMessage[] = [];
      let cursor = newest.seq;
      let more = true;
      while (more) {
        const page = await client.chat.load({
          id: chatId,
          afterSeq: cursor,
          limit: FULL_LOAD_PAGE_SIZE,
        });
        collected.push(...page.messages);
        const pageNewest = page.messages[page.messages.length - 1];
        more = page.hasMoreAfter && !!pageNewest && pageNewest.seq !== cursor;
        cursor = pageNewest?.seq ?? cursor;
      }
      return (prev) => ({
        ...prev,
        messages: mergeSorted(prev.messages, collected),
        hasMoreAfter: false,
      });
    });
  }, [state, runIncrementalLoad, client, chatId]);

  // Loads the ENTIRE gap after `afterSeq`: pages forward until the walk
  // reaches the next already-loaded message, so one click closes the break.
  const loadGap = useCallback(
    (afterSeq: number) => {
      if (!state) return;
      // The message immediately after the gap, by seq (messages stay sorted
      // ascending via mergeSorted, so find() is enough).
      const nextSeq = state.messages.find((m) => m.seq > afterSeq)?.seq;
      runIncrementalLoad(`gap:${afterSeq}`, async () => {
        const collected: ChatMessage[] = [];
        let cursor = afterSeq;
        let more = true;
        while (more) {
          const page = await client.chat.load({
            id: chatId,
            afterSeq: cursor,
            limit: FULL_LOAD_PAGE_SIZE,
          });
          collected.push(...page.messages);
          const pageNewest = page.messages[page.messages.length - 1];
          const reachedNext =
            nextSeq !== undefined && !!pageNewest && pageNewest.seq >= nextSeq;
          more =
            !reachedNext &&
            page.hasMoreAfter &&
            !!pageNewest &&
            pageNewest.seq !== cursor;
          cursor = pageNewest?.seq ?? cursor;
        }
        return (prev) => {
          const gaps = new Set(prev.gaps);
          gaps.delete(afterSeq);
          return {
            ...prev,
            messages: mergeSorted(prev.messages, collected),
            gaps,
          };
        };
      });
    },
    [state, runIncrementalLoad, client, chatId],
  );

  // Loads the chat's complete history in one action: full from-start walk
  // merged over the window, clearing every gap and both edges.
  const loadAll = useCallback(() => {
    if (!state) return;
    runIncrementalLoad("all", async () => {
      const all = await fetchFullTranscript(client, chatId);
      return (prev) => ({
        messages: mergeSorted(prev.messages, all),
        gaps: new Set<number>(),
        hasMoreBefore: false,
        hasMoreAfter: false,
      });
    });
  }, [state, runIncrementalLoad, client, chatId]);

  return {
    chat: base.data,
    messages: state?.messages ?? [],
    gaps: state?.gaps ?? new Set(),
    hasMoreBefore: state?.hasMoreBefore ?? false,
    hasMoreAfter: state?.hasMoreAfter ?? false,
    isForbidden:
      base.error instanceof GramError && base.error.statusCode === 403,
    loadBefore,
    loadAfter,
    loadAllBefore,
    loadAllAfter,
    loadGap,
    loadAll,
    loadingKey,
    isLoading: base.isLoading,
    isError: base.isError || loadError,
  };
}
