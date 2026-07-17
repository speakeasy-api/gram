import { useEffect, useMemo, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import type { Chat } from "@gram/client/models/components/chat.js";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useSdkClient } from "@/contexts/Sdk";
import { FULL_LOAD_PAGE_SIZE } from "./transcriptFull";

// Page size for keyset pagination. Kept under the server's max (200) so the
// initial paint is cheap on long chats; further pages stream in on scroll.
export const TRANSCRIPT_PAGE_SIZE = 50;

// Cursor encodes which keyset edge to fetch. "start" requests the oldest page
// (the beginning of the thread) so a normal transcript opens at the first
// message and reads forward; "before"/"after" page older/newer by seq.
type Cursor =
  | { dir: "start" }
  | { dir: "before"; seq: number }
  | { dir: "after"; seq: number };

export interface ChatTranscript {
  chat: Chat | undefined;
  messages: ChatMessage[];
  isLoading: boolean;
  isError: boolean;
  /** True when the load failed specifically because the caller lacks
   * chat:read for this chat, so callers can show a permission message
   * instead of a generic error. */
  isForbidden: boolean;
  /** Older messages exist above the first loaded message. */
  hasMoreBefore: boolean;
  /** Newer messages exist below the last loaded message. */
  hasMoreAfter: boolean;
  fetchOlder: () => void;
  fetchNewer: () => void;
  isFetchingOlder: boolean;
  isFetchingNewer: boolean;
  /** Load every remaining message below the loaded window (the from-start
   * transcript's only missing range, so this completes the conversation). */
  loadRest: () => void;
  isLoadingRest: boolean;
}

// useChatTranscript paginates a chat's latest generation by seq keyset. The
// initial page is the start of the thread; scrolling streams further pages in,
// and loadRest drains every remaining message on demand (the transcript never
// auto-loads the whole history).
export function useChatTranscript(
  chatId: string,
  enabled: boolean,
): ChatTranscript {
  const client = useSdkClient();

  const query = useInfiniteQuery({
    queryKey: ["chat", chatId, "transcript"],
    enabled,
    initialPageParam: { dir: "start" } as Cursor,
    queryFn: ({ pageParam }) => {
      const request: {
        id: string;
        limit: number;
        beforeSeq?: number;
        afterSeq?: number;
        fromStart?: boolean;
      } = { id: chatId, limit: TRANSCRIPT_PAGE_SIZE };
      if (pageParam.dir === "start") request.fromStart = true;
      if (pageParam.dir === "before") request.beforeSeq = pageParam.seq;
      if (pageParam.dir === "after") {
        request.afterSeq = pageParam.seq;
        // Forward pages also serve the load-the-rest drain, so use the server's
        // max page to finish in as few round trips as possible.
        request.limit = FULL_LOAD_PAGE_SIZE;
      }
      return client.chat.load(request);
    },
    // "previous" = older. firstPage is the oldest page currently held.
    getPreviousPageParam: (firstPage): Cursor | undefined => {
      const oldest = firstPage.messages[0];
      if (!firstPage.hasMoreBefore || !oldest) return undefined;
      return { dir: "before", seq: oldest.seq };
    },
    // "next" = newer. lastPage is the newest page currently held.
    getNextPageParam: (lastPage): Cursor | undefined => {
      const newest = lastPage.messages[lastPage.messages.length - 1];
      if (!lastPage.hasMoreAfter || !newest) return undefined;
      return { dir: "after", seq: newest.seq };
    },
    // Let 404s and 403s fall through to the panel's "Not found" / "Permission
    // denied" UI instead of throwing to the nearest error boundary — both are
    // anticipated outcomes of opening an arbitrary chat_id, not unexpected
    // failures.
    throwOnError: (error) =>
      !(
        error instanceof GramError &&
        (error.statusCode === 404 || error.statusCode === 403)
      ),
  });

  // On-demand drain: while set, keep pulling forward pages until the server
  // reports nothing newer. Only ever started by loadRest (a user action), so
  // the transcript stays lazily paginated by default. Errors stop the drain
  // (the transcript's break divider stays as a manual retry).
  const [draining, setDraining] = useState(false);
  useEffect(() => setDraining(false), [chatId]);
  const { hasNextPage, isFetchingNextPage, isError, fetchNextPage } = query;
  useEffect(() => {
    if (!draining) return;
    if (isError || !hasNextPage) {
      setDraining(false);
      return;
    }
    if (!isFetchingNextPage) void fetchNextPage();
  }, [draining, hasNextPage, isFetchingNextPage, isError, fetchNextPage]);

  const messages = useMemo(
    () => (query.data?.pages ?? []).flatMap((p) => p.messages),
    [query.data],
  );

  // Chat-level enrichment (cost, agent usage, source) is only populated on the
  // initial from-start page; locate it by its page param rather than by index,
  // since appends/prepends shift indices.
  const chat = useMemo(() => {
    const data = query.data;
    if (!data) return undefined;
    const params = data.pageParams as Cursor[];
    const idx = params.findIndex((p) => p.dir === "start");
    return data.pages[idx >= 0 ? idx : 0];
  }, [query.data]);

  return {
    chat,
    messages,
    isLoading: query.isLoading,
    isError: query.isError,
    isForbidden:
      query.error instanceof GramError && query.error.statusCode === 403,
    hasMoreBefore: query.hasPreviousPage,
    hasMoreAfter: query.hasNextPage,
    fetchOlder: () => {
      if (query.hasPreviousPage && !query.isFetchingPreviousPage) {
        void query.fetchPreviousPage();
      }
    },
    fetchNewer: () => {
      if (query.hasNextPage && !query.isFetchingNextPage) {
        void query.fetchNextPage();
      }
    },
    isFetchingOlder: query.isFetchingPreviousPage,
    isFetchingNewer: query.isFetchingNextPage,
    loadRest: () => setDraining(true),
    isLoadingRest: draining,
  };
}
