import { Page } from "@/components/page-layout";
import { SearchBar } from "@/components/ui/search-bar";
import { telemetrySearchChats } from "@gram/client/funcs/telemetrySearchChats";
import { ChatSummary } from "@gram/client/models/components";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery } from "@tanstack/react-query";
import { XIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { ConversationRow } from "./ConversationRow";
import {
  ConversationDetail,
  ConversationDetailEmpty,
} from "./ConversationDetail";

const perPage = 25;

export default function ConversationsPage() {
  const [searchQuery, setSearchQuery] = useState<string | null>(null);
  const [searchInput, setSearchInput] = useState("");
  const [selectedChat, setSelectedChat] = useState<ChatSummary | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const client = useGramContext();

  const {
    data,
    error,
    fetchNextPage,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
  } = useInfiniteQuery({
    queryKey: ["conversations", searchQuery],
    queryFn: ({ pageParam }) =>
      unwrapAsync(
        telemetrySearchChats(client, {
          searchToolCallsPayload: {
            filter: searchQuery ? { gramUrn: searchQuery } : undefined,
            cursor: pageParam,
            limit: perPage,
            sort: "desc",
          },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  });

  const allChats = data?.pages.flatMap((page) => page.chats) ?? [];

  // Debounce search input
  useEffect(() => {
    const timeoutId = setTimeout(() => {
      setSearchQuery(searchInput || null);
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [searchInput]);

  // Handle scroll for infinite loading
  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const container = e.currentTarget;
    const distanceFromBottom =
      container.scrollHeight - (container.scrollTop + container.clientHeight);
    if (isFetchingNextPage || isFetching || !hasNextPage) return;
    if (distanceFromBottom < 200) {
      fetchNextPage();
    }
  };

  const isLoading = isFetching && allChats.length === 0;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
      </Page.Header>
      <Page.Body fullWidth fullHeight className="!p-0">
        <div className="flex flex-row h-full w-full">
          {/* List Panel */}
          <div className="flex flex-col gap-4 w-[420px] min-w-[360px] border-r border-border">
            {/* Search */}
            <div className="px-4 pt-4">
              <SearchBar
                value={searchInput}
                onChange={setSearchInput}
                placeholder="Search by URN..."
                className="w-full"
              />
            </div>

            {/* Chat list */}
            <div className="flex-1 overflow-hidden flex flex-col">
              {/* Loading indicator */}
              {isFetching && allChats.length > 0 && (
                <div className="h-0.5 bg-primary-default/20">
                  <div className="h-full bg-primary-default animate-pulse" />
                </div>
              )}

              <div
                ref={containerRef}
                className="overflow-y-auto flex-1"
                onScroll={handleScroll}
              >
                {error ? (
                  <div className="flex flex-col items-center gap-2 py-12">
                    <XIcon className="size-6 stroke-destructive-default" />
                    <span className="text-destructive-default font-medium">
                      Error loading conversations
                    </span>
                    <span className="text-sm text-muted-foreground">
                      {error instanceof Error
                        ? error.message
                        : "An unexpected error occurred"}
                    </span>
                  </div>
                ) : isLoading ? (
                  <div className="flex items-center justify-center gap-2 py-12 text-muted-foreground">
                    <Icon
                      name="loader-circle"
                      className="size-4 animate-spin"
                    />
                    <span>Loading conversations...</span>
                  </div>
                ) : allChats.length === 0 ? (
                  <div className="py-12 text-center text-muted-foreground text-sm">
                    {searchQuery
                      ? "No conversations match your search"
                      : "No conversations found"}
                  </div>
                ) : (
                  <>
                    {allChats.map((chat) => (
                      <ConversationRow
                        key={chat.gramChatId}
                        chat={chat}
                        isSelected={
                          selectedChat?.gramChatId === chat.gramChatId
                        }
                        onSelect={() => setSelectedChat(chat)}
                      />
                    ))}

                    {isFetchingNextPage && (
                      <div className="flex items-center justify-center gap-2 py-4 text-muted-foreground border-t border-border">
                        <Icon
                          name="loader-circle"
                          className="size-4 animate-spin"
                        />
                        <span className="text-sm">Loading more...</span>
                      </div>
                    )}
                  </>
                )}
              </div>

              {/* Footer */}
              {allChats.length > 0 && (
                <div className="px-4 py-2 bg-surface-secondary-default border-t border-border text-xs text-muted-foreground">
                  {allChats.length}{" "}
                  {allChats.length === 1 ? "conversation" : "conversations"}
                  {hasNextPage && " · Scroll to load more"}
                </div>
              )}
            </div>
          </div>

          {/* Detail Panel */}
          <div className="flex-1 min-w-0 bg-surface-default">
            {selectedChat ? (
              <ConversationDetail chat={selectedChat} />
            ) : (
              <ConversationDetailEmpty />
            )}
          </div>
        </div>
      </Page.Body>
    </Page>
  );
}
