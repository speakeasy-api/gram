import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListRiskPoliciesRequest, ListRiskPoliciesSecurity } from "../models/operations/listriskpolicies.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildRiskListPoliciesQuery, prefetchRiskListPolicies, queryKeyRiskListPolicies, RiskListPoliciesQueryData } from "./riskListPolicies.core.js";
export { buildRiskListPoliciesQuery, prefetchRiskListPolicies, queryKeyRiskListPolicies, type RiskListPoliciesQueryData, };
export type RiskListPoliciesQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listRiskPolicies risk
 *
 * @remarks
 * List all risk analysis policies for the current project.
 */
export declare function useRiskListPolicies(request?: ListRiskPoliciesRequest | undefined, security?: ListRiskPoliciesSecurity | undefined, options?: QueryHookOptions<RiskListPoliciesQueryData, RiskListPoliciesQueryError>): UseQueryResult<RiskListPoliciesQueryData, RiskListPoliciesQueryError>;
/**
 * listRiskPolicies risk
 *
 * @remarks
 * List all risk analysis policies for the current project.
 */
export declare function useRiskListPoliciesSuspense(request?: ListRiskPoliciesRequest | undefined, security?: ListRiskPoliciesSecurity | undefined, options?: SuspenseQueryHookOptions<RiskListPoliciesQueryData, RiskListPoliciesQueryError>): UseSuspenseQueryResult<RiskListPoliciesQueryData, RiskListPoliciesQueryError>;
export declare function setRiskListPoliciesData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: RiskListPoliciesQueryData): RiskListPoliciesQueryData | undefined;
export declare function invalidateRiskListPolicies(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllRiskListPolicies(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=riskListPolicies.d.ts.map