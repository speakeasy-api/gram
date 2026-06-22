import { useMemo } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import type { Chat, ChatMessage } from "@gram/client/models/components";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useSdkClient } from "@/contexts/Sdk";

// Page size for keyset pagination. Kept under the server's max (200) so the
// initial paint is cheap on long chats; older pages stream in on scroll.
export const TRANSCRIPT_PAGE_SIZE = 50;

// Cursor encodes which keyset edge to fetch. "initial" omits cursors and gets
// the newest page; "before"/"after" page older/newer by seq.
type Cursor =
  | { dir: "initial" }
  | { dir: "before"; seq: number }
  | { dir: "after"; seq: number };

export interface ChatTranscript {
  chat: Chat | undefined;
  messages: ChatMessage[];
  isLoading: boolean;
  isError: boolean;
  /** Older messages exist above the first loaded message. */
  hasMoreBefore: boolean;
  /** Newer messages exist below the last loaded message. */
  hasMoreAfter: boolean;
  fetchOlder: () => void;
  fetchNewer: () => void;
  isFetchingOlder: boolean;
  isFetchingNewer: boolean;
}

// useChatTranscript paginates a chat's latest generation by seq keyset. The
// initial page is the newest slice; scrolling up fetches older pages (mapped to
// React Query's "previous" direction) and the rare scroll-down tail fetches
// newer pages ("next"). Pages are stored oldest-first so flattening yields a
// single ascending transcript.
export function useChatTranscript(
  chatId: string,
  enabled: boolean,
): ChatTranscript {
  const client = useSdkClient();

  const query = useInfiniteQuery({
    queryKey: ["chat", chatId, "transcript"],
    enabled,
    initialPageParam: { dir: "initial" } as Cursor,
    queryFn: ({ pageParam }) => {
      const request: {
        id: string;
        limit: number;
        beforeSeq?: number;
        afterSeq?: number;
      } = { id: chatId, limit: TRANSCRIPT_PAGE_SIZE };
      if (pageParam.dir === "before") request.beforeSeq = pageParam.seq;
      if (pageParam.dir === "after") request.afterSeq = pageParam.seq;
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
    // Let 404s fall through to the panel's "Not found" UI.
    throwOnError: (error) =>
      !(error instanceof GramError && error.statusCode === 404),
  });

  const messages = useMemo(
    () => (query.data?.pages ?? []).flatMap((p) => p.messages),
    [query.data],
  );

  // Chat-level enrichment (cost, agent usage, source) is only populated on the
  // initial newest page; locate it by its page param rather than by index,
  // since prepends shift indices.
  const chat = useMemo(() => {
    const data = query.data;
    if (!data) return undefined;
    const params = data.pageParams as Cursor[];
    const idx = params.findIndex((p) => p.dir === "initial");
    return data.pages[idx >= 0 ? idx : data.pages.length - 1];
  }, [query.data]);

  return {
    chat,
    messages,
    isLoading: query.isLoading,
    isError: query.isError,
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
  };
}
