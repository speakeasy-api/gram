import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolFiltersResult } from "../models/components/listtoolfiltersresult.js";
import {
  ListToolsetToolFiltersRequest,
  ListToolsetToolFiltersSecurity,
} from "../models/operations/listtoolsettoolfilters.js";
export type ListToolsetToolFiltersQueryData = ListToolFiltersResult;
export declare function prefetchListToolsetToolFilters(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListToolsetToolFiltersRequest,
  security?: ListToolsetToolFiltersSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListToolsetToolFiltersQuery(
  client$: GramCore,
  request: ListToolsetToolFiltersRequest,
  security?: ListToolsetToolFiltersSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ListToolsetToolFiltersQueryData>;
};
export declare function queryKeyListToolsetToolFilters(parameters: {
  slug: string;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listToolsetToolFilters.core.d.ts.map
