import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskCategoriesResult } from "../models/components/riskcategoriesresult.js";
import {
  ListRiskCategoriesRequest,
  ListRiskCategoriesSecurity,
} from "../models/operations/listriskcategories.js";
export type RiskCategoriesQueryData = RiskCategoriesResult;
export declare function prefetchRiskCategories(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListRiskCategoriesRequest | undefined,
  security?: ListRiskCategoriesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRiskCategoriesQuery(
  client$: GramCore,
  request?: ListRiskCategoriesRequest | undefined,
  security?: ListRiskCategoriesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<RiskCategoriesQueryData>;
};
export declare function queryKeyRiskCategories(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskCategories.core.d.ts.map
