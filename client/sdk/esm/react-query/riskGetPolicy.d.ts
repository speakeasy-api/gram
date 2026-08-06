import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildRiskGetPolicyQuery, prefetchRiskGetPolicy, queryKeyRiskGetPolicy, RiskGetPolicyQueryData } from "./riskGetPolicy.core.js";
export { buildRiskGetPolicyQuery, prefetchRiskGetPolicy, queryKeyRiskGetPolicy, type RiskGetPolicyQueryData, };
export type RiskGetPolicyQueryError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getRiskPolicy risk
 *
 * @remarks
 * Get a risk analysis policy by ID.
 */
export declare function useRiskGetPolicy(request: operations.GetRiskPolicyRequest, security?: operations.GetRiskPolicySecurity | undefined, options?: QueryHookOptions<RiskGetPolicyQueryData, RiskGetPolicyQueryError>): UseQueryResult<RiskGetPolicyQueryData, RiskGetPolicyQueryError>;
/**
 * getRiskPolicy risk
 *
 * @remarks
 * Get a risk analysis policy by ID.
 */
export declare function useRiskGetPolicySuspense(request: operations.GetRiskPolicyRequest, security?: operations.GetRiskPolicySecurity | undefined, options?: SuspenseQueryHookOptions<RiskGetPolicyQueryData, RiskGetPolicyQueryError>): UseSuspenseQueryResult<RiskGetPolicyQueryData, RiskGetPolicyQueryError>;
export declare function setRiskGetPolicyData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: RiskGetPolicyQueryData): RiskGetPolicyQueryData | undefined;
export declare function invalidateRiskGetPolicy(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllRiskGetPolicy(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=riskGetPolicy.d.ts.map