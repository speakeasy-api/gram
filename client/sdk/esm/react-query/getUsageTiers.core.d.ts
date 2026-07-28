import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UsageTiers } from "../models/components/usagetiers.js";
export type GetUsageTiersQueryData = UsageTiers;
export declare function prefetchGetUsageTiers(
  queryClient: QueryClient,
  client$: GramCore,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetUsageTiersQuery(
  client$: GramCore,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<GetUsageTiersQueryData>;
};
export declare function queryKeyGetUsageTiers(): QueryKey;
//# sourceMappingURL=getUsageTiers.core.d.ts.map
