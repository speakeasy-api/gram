import { Page } from "@/components/page-layout";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import type { BusinessMemory } from "@gram/client/models/components/businessmemory.js";
import { useChatDeleteMutation } from "@gram/client/react-query/chatDelete.js";
import { useListBusinessMemoryContentScopes } from "@gram/client/react-query/listBusinessMemoryContentScopes.js";
import { useListBusinessMemoriesInfinite } from "@gram/client/react-query/listBusinessMemories.js";
import { invalidateAllListChats } from "@gram/client/react-query/listChats.js";
import { useSearchBusinessMemories } from "@gram/client/react-query/searchBusinessMemories.js";
import { Badge, Button, Column, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { BusinessMemoryScopeTree } from "./BusinessMemoryScopeTree";
import {
  buildScopeTree,
  scopeSelectionToFilter,
  type ScopeSelection,
} from "./businessMemoryScopes";

const PAGE_SIZE = 100;
const SEARCH_LIMIT = 100;

function memoryTypeVariant(
  memoryType: BusinessMemory["memoryType"],
): "information" | "success" | "neutral" {
  switch (memoryType) {
    case "glossary":
      return "information";
    case "procedure":
      return "success";
    case "result":
      return "neutral";
  }
}

function formatSimilarity(similarity: number | undefined): string {
  if (similarity === undefined) return "—";
  return `${Math.round(similarity * 100)}%`;
}

const columns: Column<BusinessMemory>[] = [
  {
    key: "body",
    header: "Memory",
    width: "2fr",
    render: (memory) => <Type>{memory.body}</Type>,
  },
  {
    key: "type",
    header: "Type",
    width: "120px",
    render: (memory) => (
      <Badge variant={memoryTypeVariant(memory.memoryType)} background>
        <Badge.Text>{memory.memoryType}</Badge.Text>
      </Badge>
    ),
  },
  {
    key: "scope",
    header: "Content scope",
    width: "1fr",
    render: (memory) => (
      <div className="flex flex-wrap gap-1">
        {memory.contentScope.length === 0 ? (
          <Type small muted>
            Unlabeled
          </Type>
        ) : (
          memory.contentScope.map((label) => (
            <Badge key={label} variant="neutral">
              <Badge.Text>{label}</Badge.Text>
            </Badge>
          ))
        )}
      </div>
    ),
  },
  {
    key: "source",
    header: "Provenance",
    width: "1fr",
    render: (memory) => (
      <div className="flex flex-col gap-1">
        <Type small>{memory.sourceAuthorId ?? "Unknown author"}</Type>
        <Type small muted>
          Chat{" "}
          {memory.sourceChatId === "unavailable"
            ? memory.sourceChatId
            : memory.sourceChatId.slice(0, 8)}
          {memory.sourceTurn === undefined
            ? ""
            : ` · turn ${memory.sourceTurn}`}
        </Type>
      </div>
    ),
  },
  {
    key: "extracted",
    header: "Extracted",
    width: "170px",
    render: (memory) => (
      <Type small>{memory.extractedAt.toLocaleString()}</Type>
    ),
  },
  {
    key: "similarity",
    header: "Similarity",
    width: "100px",
    render: (memory) => (
      <Type small>{formatSimilarity(memory.similarity)}</Type>
    ),
  },
];

export function BusinessMemoryCorpus(): JSX.Element {
  const [search, setSearch] = useState("");
  const [scopeSelection, setScopeSelection] = useState<ScopeSelection | null>(
    null,
  );
  const [selectedMemory, setSelectedMemory] = useState<BusinessMemory | null>(
    null,
  );
  const queryClient = useQueryClient();
  const deleteChat = useChatDeleteMutation();
  const normalizedSearch = search.trim();
  const scopeFilter = scopeSelectionToFilter(scopeSelection);
  const listQuery = useListBusinessMemoriesInfinite(
    { limit: PAGE_SIZE, ...scopeFilter },
    undefined,
    {
      enabled: normalizedSearch.length === 0,
      throwOnError: false,
    },
  );
  const scopeQuery = useListBusinessMemoryContentScopes(undefined, undefined, {
    throwOnError: false,
  });
  const searchQuery = useSearchBusinessMemories(
    { query: normalizedSearch, limit: SEARCH_LIMIT, ...scopeFilter },
    undefined,
    {
      enabled: normalizedSearch.length > 0,
      throwOnError: false,
    },
  );

  const listedMemories = useMemo(
    () => listQuery.data?.pages.flatMap((page) => page.result.memories) ?? [],
    [listQuery.data?.pages],
  );
  const searchedMemories = searchQuery.data?.memories;
  const memories = useMemo(
    () =>
      normalizedSearch.length > 0 ? (searchedMemories ?? []) : listedMemories,
    [normalizedSearch.length, searchedMemories, listedMemories],
  );
  const scopeTree = useMemo(
    () => buildScopeTree(scopeQuery.data?.nodes ?? []),
    [scopeQuery.data?.nodes],
  );
  const loading =
    normalizedSearch.length > 0 ? searchQuery.isLoading : listQuery.isLoading;
  const error =
    normalizedSearch.length > 0 ? searchQuery.error : listQuery.error;

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h2 className="text-lg font-semibold">Extracted memories</h2>
        <p className="text-muted-foreground mt-1 text-sm">
          Inspect the raw validation corpus or run semantic search using the
          stored embeddings. Search performs an embedding-model call and is not
          representative of the future prompt-time read path.
        </p>
      </div>

      <div className="grid min-w-0 grid-cols-1 gap-4 lg:grid-cols-[240px_minmax(0,1fr)]">
        <BusinessMemoryScopeTree
          nodes={scopeTree}
          selection={scopeSelection}
          onSelectionChange={setScopeSelection}
          totalMemories={scopeQuery.data?.totalMemories ?? 0}
          loading={scopeQuery.isLoading}
          error={Boolean(scopeQuery.error)}
        />

        <div className="flex min-w-0 flex-col gap-4">
          <Page.Toolbar>
            <Page.Toolbar.Search
              value={search}
              onChange={setSearch}
              placeholder="Semantic search memories…"
              debounceMs={400}
            />
            {!loading && (
              <Page.Toolbar.Count>
                {memories.length}{" "}
                {memories.length === 1 ? "memory" : "memories"}
              </Page.Toolbar.Count>
            )}
            <Page.Toolbar.Refresh
              onRefresh={() => {
                if (normalizedSearch.length > 0) {
                  void searchQuery.refetch();
                } else {
                  void listQuery.refetch();
                }
                void scopeQuery.refetch();
              }}
              isRefreshing={
                listQuery.isFetching ||
                searchQuery.isFetching ||
                scopeQuery.isFetching
              }
            />
          </Page.Toolbar>

          {loading ? (
            <SkeletonTable />
          ) : error ? (
            <div className="border-border rounded-md border p-6">
              <Type className="text-destructive">
                {error.message || "Failed to load business memories."}
              </Type>
            </div>
          ) : (
            <>
              <Table
                columns={columns}
                data={memories}
                rowKey={(memory) => memory.id}
                onRowClick={(memory) => {
                  if (
                    memory.sourceChatId === "unavailable" ||
                    memory.sourceTurn === undefined
                  ) {
                    return;
                  }
                  setSelectedMemory(memory);
                }}
                noResultsMessage={
                  <Type>
                    {scopeSelection
                      ? "No memories match this content scope."
                      : normalizedSearch.length > 0
                        ? "No semantically similar memories found."
                        : "No memories extracted yet."}
                  </Type>
                }
              />
              {normalizedSearch.length === 0 && listQuery.hasNextPage && (
                <div className="flex justify-center">
                  <Button
                    variant="tertiary"
                    size="sm"
                    disabled={listQuery.isFetchingNextPage}
                    onClick={() => void listQuery.fetchNextPage()}
                  >
                    {listQuery.isFetchingNextPage ? "Loading…" : "Load more"}
                  </Button>
                </div>
              )}
            </>
          )}
        </div>
      </div>

      <ChatDetailSheet
        chatId={
          selectedMemory?.sourceChatId === "unavailable"
            ? null
            : (selectedMemory?.sourceChatId ?? null)
        }
        focusedMessageTurn={selectedMemory?.sourceTurn}
        onClose={() => setSelectedMemory(null)}
        onDelete={(chatId) => {
          deleteChat.mutate(
            { request: { id: chatId } },
            {
              onSuccess: () => {
                void invalidateAllListChats(queryClient);
                void listQuery.refetch();
                void scopeQuery.refetch();
                if (normalizedSearch.length > 0) {
                  void searchQuery.refetch();
                }
                setSelectedMemory(null);
              },
            },
          );
        }}
      />
    </div>
  );
}
