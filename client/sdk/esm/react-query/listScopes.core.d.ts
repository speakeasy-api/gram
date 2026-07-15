import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListScopesResult } from "../models/components/listscopesresult.js";
import {
  ListScopesRequest,
  ListScopesSecurity,
} from "../models/operations/listscopes.js";
export type ListScopesQueryData = ListScopesResult;
export declare function prefetchListScopes(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListScopesRequest | undefined,
  security?: ListScopesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListScopesQuery(
  client$: GramCore,
  request?: ListScopesRequest | undefined,
  security?: ListScopesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ListScopesQueryData>;
};
export declare function queryKeyListScopes(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listScopes.core.d.ts.map
