import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListHooksTracesResult } from "../models/components/listhookstracesresult.js";
import {
  ListHooksTracesRequest,
  ListHooksTracesSecurity,
} from "../models/operations/listhookstraces.js";
export type ListHooksTracesQueryData = ListHooksTracesResult;
export declare function prefetchListHooksTraces(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListHooksTracesRequest,
  security?: ListHooksTracesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListHooksTracesQuery(
  client$: GramCore,
  request: ListHooksTracesRequest,
  security?: ListHooksTracesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ListHooksTracesQueryData>;
};
export declare function queryKeyListHooksTraces(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listHooksTraces.core.d.ts.map
