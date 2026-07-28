import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolsetsResult } from "../models/components/listtoolsetsresult.js";
import {
  ListToolsetsRequest,
  ListToolsetsSecurity,
} from "../models/operations/listtoolsets.js";
export type ListToolsetsQueryData = ListToolsetsResult;
export declare function prefetchListToolsets(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListToolsetsRequest | undefined,
  security?: ListToolsetsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListToolsetsQuery(
  client$: GramCore,
  request?: ListToolsetsRequest | undefined,
  security?: ListToolsetsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ListToolsetsQueryData>;
};
export declare function queryKeyListToolsets(parameters: {
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listToolsets.core.d.ts.map
