import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type ListSlackAppsQueryData = components.ListSlackAppsResult;
export declare function prefetchListSlackApps(
  queryClient: QueryClient,
  client$: GramCore,
  request?: operations.ListSlackAppsRequest | undefined,
  security?: operations.ListSlackAppsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListSlackAppsQuery(
  client$: GramCore,
  request?: operations.ListSlackAppsRequest | undefined,
  security?: operations.ListSlackAppsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ListSlackAppsQueryData>;
};
export declare function queryKeyListSlackApps(parameters: {
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listSlackApps.core.d.ts.map
