import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolVariationGroupsResult } from "../models/components/listtoolvariationgroupsresult.js";
import {
  ListToolVariationGroupsRequest,
  ListToolVariationGroupsSecurity,
} from "../models/operations/listtoolvariationgroups.js";
export type ToolVariationGroupsQueryData = ListToolVariationGroupsResult;
export declare function prefetchToolVariationGroups(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListToolVariationGroupsRequest | undefined,
  security?: ListToolVariationGroupsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildToolVariationGroupsQuery(
  client$: GramCore,
  request?: ListToolVariationGroupsRequest | undefined,
  security?: ListToolVariationGroupsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ToolVariationGroupsQueryData>;
};
export declare function queryKeyToolVariationGroups(parameters: {
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=toolVariationGroups.core.d.ts.map
