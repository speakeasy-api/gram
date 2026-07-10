import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetRiskPolicyRequest, GetRiskPolicySecurity } from "../models/operations/getriskpolicy.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildRiskPoliciesGetQuery, prefetchRiskPoliciesGet, queryKeyRiskPoliciesGet, RiskPoliciesGetQueryData } from "./riskPoliciesGet.core.js";
export { buildRiskPoliciesGetQuery, prefetchRiskPoliciesGet, queryKeyRiskPoliciesGet, type RiskPoliciesGetQueryData, };
export type RiskPoliciesGetQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getRiskPolicy risk
 *
 * @remarks
 * Get a risk analysis policy by ID.
 */
export declare function useRiskPoliciesGet(request: GetRiskPolicyRequest, security?: GetRiskPolicySecurity | undefined, options?: QueryHookOptions<RiskPoliciesGetQueryData, RiskPoliciesGetQueryError>): UseQueryResult<RiskPoliciesGetQueryData, RiskPoliciesGetQueryError>;
/**
 * getRiskPolicy risk
 *
 * @remarks
 * Get a risk analysis policy by ID.
 */
export declare function useRiskPoliciesGetSuspense(request: GetRiskPolicyRequest, security?: GetRiskPolicySecurity | undefined, options?: SuspenseQueryHookOptions<RiskPoliciesGetQueryData, RiskPoliciesGetQueryError>): UseSuspenseQueryResult<RiskPoliciesGetQueryData, RiskPoliciesGetQueryError>;
export declare function setRiskPoliciesGetData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: RiskPoliciesGetQueryData): RiskPoliciesGetQueryData | undefined;
export declare function invalidateRiskPoliciesGet(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllRiskPoliciesGet(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=riskPoliciesGet.d.ts.map