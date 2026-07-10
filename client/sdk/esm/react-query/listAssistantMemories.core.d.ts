import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ListAssistantMemoriesRequest,
  ListAssistantMemoriesResponse,
  ListAssistantMemoriesSecurity,
} from "../models/operations/listassistantmemories.js";
import { PageIterator } from "../types/operations.js";
export type ListAssistantMemoriesQueryData = ListAssistantMemoriesResponse;
export type ListAssistantMemoriesInfiniteQueryData = PageIterator<
  ListAssistantMemoriesResponse,
  {
    cursor: string;
  }
>;
export type ListAssistantMemoriesPageParams = PageIterator<
  ListAssistantMemoriesResponse,
  {
    cursor: string;
  }
>["~next"];
export declare function prefetchListAssistantMemories(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListAssistantMemoriesRequest,
  security?: ListAssistantMemoriesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function prefetchListAssistantMemoriesInfinite(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListAssistantMemoriesRequest,
  security?: ListAssistantMemoriesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListAssistantMemoriesQuery(
  client$: GramCore,
  request: ListAssistantMemoriesRequest,
  security?: ListAssistantMemoriesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ListAssistantMemoriesQueryData>;
};
export declare function buildListAssistantMemoriesInfiniteQuery(
  client$: GramCore,
  request: ListAssistantMemoriesRequest,
  security?: ListAssistantMemoriesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext<QueryKey, ListAssistantMemoriesPageParams>,
  ) => Promise<ListAssistantMemoriesInfiniteQueryData>;
};
export declare function queryKeyListAssistantMemories(parameters: {
  assistantId: string;
  tags?: Array<string> | undefined;
  includeDeleted?: boolean | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
export declare function queryKeyListAssistantMemoriesInfinite(parameters: {
  assistantId: string;
  tags?: Array<string> | undefined;
  includeDeleted?: boolean | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listAssistantMemories.core.d.ts.map
