import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SearchUsersResult } from "../models/components/searchusersresult.js";
import {
  SearchUsersRequest,
  SearchUsersSecurity,
} from "../models/operations/searchusers.js";
export type SearchUsersQueryData = SearchUsersResult;
export declare function prefetchSearchUsers(
  queryClient: QueryClient,
  client$: GramCore,
  request: SearchUsersRequest,
  security?: SearchUsersSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildSearchUsersQuery(
  client$: GramCore,
  request: SearchUsersRequest,
  security?: SearchUsersSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<SearchUsersQueryData>;
};
export declare function queryKeySearchUsers(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=searchUsers.core.d.ts.map
