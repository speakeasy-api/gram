import {
  InvalidateQueryFilters,
  QueryClient,
  QueryFunctionContext,
  QueryKey,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
export type DraftToolsetQueryData = components.Toolset;
/**
 * getDraftToolset toolsets
 *
 * @remarks
 * Get the draft version of a toolset for preview/staging. Returns the toolset with draft tool URNs instead of production.
 */
export declare function useDraftToolset(
  request: operations.GetDraftToolsetRequest,
  security?: operations.GetDraftToolsetSecurity | undefined,
  options?: QueryHookOptions<DraftToolsetQueryData>,
): UseQueryResult<DraftToolsetQueryData, Error>;
/**
 * getDraftToolset toolsets
 *
 * @remarks
 * Get the draft version of a toolset for preview/staging. Returns the toolset with draft tool URNs instead of production.
 */
export declare function useDraftToolsetSuspense(
  request: operations.GetDraftToolsetRequest,
  security?: operations.GetDraftToolsetSecurity | undefined,
  options?: SuspenseQueryHookOptions<DraftToolsetQueryData>,
): UseSuspenseQueryResult<DraftToolsetQueryData, Error>;
export declare function prefetchDraftToolset(
  queryClient: QueryClient,
  client$: GramCore,
  request: operations.GetDraftToolsetRequest,
  security?: operations.GetDraftToolsetSecurity | undefined,
): Promise<void>;
export declare function setDraftToolsetData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      slug: string;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: DraftToolsetQueryData,
): DraftToolsetQueryData | undefined;
export declare function invalidateDraftToolset(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        slug: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllDraftToolset(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function buildDraftToolsetQuery(
  client$: GramCore,
  request: operations.GetDraftToolsetRequest,
  security?: operations.GetDraftToolsetSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<DraftToolsetQueryData>;
};
export declare function queryKeyDraftToolset(parameters: {
  slug: string;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=draftToolset.d.ts.map
