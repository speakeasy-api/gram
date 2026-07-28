import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SearchChatsResult } from "../models/components/searchchatsresult.js";
import {
  SearchChatsRequest,
  SearchChatsSecurity,
} from "../models/operations/searchchats.js";
export type SearchChatsQueryData = SearchChatsResult;
export declare function prefetchSearchChats(
  queryClient: QueryClient,
  client$: GramCore,
  request: SearchChatsRequest,
  security?: SearchChatsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildSearchChatsQuery(
  client$: GramCore,
  request: SearchChatsRequest,
  security?: SearchChatsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<SearchChatsQueryData>;
};
export declare function queryKeySearchChats(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=searchChats.core.d.ts.map
