import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListFilterOptionsResult } from "../models/components/listfilteroptionsresult.js";
import {
  ListFilterOptionsRequest,
  ListFilterOptionsSecurity,
} from "../models/operations/listfilteroptions.js";
export type ListFilterOptionsQueryData = ListFilterOptionsResult;
export declare function prefetchListFilterOptions(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListFilterOptionsRequest,
  security?: ListFilterOptionsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListFilterOptionsQuery(
  client$: GramCore,
  request: ListFilterOptionsRequest,
  security?: ListFilterOptionsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ListFilterOptionsQueryData>;
};
export declare function queryKeyListFilterOptions(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listFilterOptions.core.d.ts.map
