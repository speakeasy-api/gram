import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListRiskEvalReviewsRequest, ListRiskEvalReviewsSecurity } from "../models/operations/listriskevalreviews.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildRiskListEvalReviewsQuery, prefetchRiskListEvalReviews, queryKeyRiskListEvalReviews, RiskListEvalReviewsQueryData } from "./riskListEvalReviews.core.js";
export { buildRiskListEvalReviewsQuery, prefetchRiskListEvalReviews, queryKeyRiskListEvalReviews, type RiskListEvalReviewsQueryData, };
export type RiskListEvalReviewsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listRiskEvalReviews risk
 *
 * @remarks
 * List the active regression set for a prompt-based policy: every reviewer's current ground-truth verdicts.
 */
export declare function useRiskListEvalReviews(request: ListRiskEvalReviewsRequest, security?: ListRiskEvalReviewsSecurity | undefined, options?: QueryHookOptions<RiskListEvalReviewsQueryData, RiskListEvalReviewsQueryError>): UseQueryResult<RiskListEvalReviewsQueryData, RiskListEvalReviewsQueryError>;
/**
 * listRiskEvalReviews risk
 *
 * @remarks
 * List the active regression set for a prompt-based policy: every reviewer's current ground-truth verdicts.
 */
export declare function useRiskListEvalReviewsSuspense(request: ListRiskEvalReviewsRequest, security?: ListRiskEvalReviewsSecurity | undefined, options?: SuspenseQueryHookOptions<RiskListEvalReviewsQueryData, RiskListEvalReviewsQueryError>): UseSuspenseQueryResult<RiskListEvalReviewsQueryData, RiskListEvalReviewsQueryError>;
export declare function setRiskListEvalReviewsData(client: QueryClient, queryKeyBase: [
    parameters: {
        policyId: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: RiskListEvalReviewsQueryData): RiskListEvalReviewsQueryData | undefined;
export declare function invalidateRiskListEvalReviews(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        policyId: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllRiskListEvalReviews(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=riskListEvalReviews.d.ts.map