import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskEvalReviewsResult } from "../models/components/listriskevalreviewsresult.js";
import { ListRiskEvalReviewsRequest, ListRiskEvalReviewsSecurity } from "../models/operations/listriskevalreviews.js";
export type RiskListEvalReviewsQueryData = ListRiskEvalReviewsResult;
export declare function prefetchRiskListEvalReviews(queryClient: QueryClient, client$: GramCore, request: ListRiskEvalReviewsRequest, security?: ListRiskEvalReviewsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskListEvalReviewsQuery(client$: GramCore, request: ListRiskEvalReviewsRequest, security?: ListRiskEvalReviewsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskListEvalReviewsQueryData>;
};
export declare function queryKeyRiskListEvalReviews(parameters: {
    policyId: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskListEvalReviews.core.d.ts.map