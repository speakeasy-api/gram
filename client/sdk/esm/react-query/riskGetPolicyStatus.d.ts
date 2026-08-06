import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildRiskGetPolicyStatusQuery, prefetchRiskGetPolicyStatus, queryKeyRiskGetPolicyStatus, RiskGetPolicyStatusQueryData } from "./riskGetPolicyStatus.core.js";
export { buildRiskGetPolicyStatusQuery, prefetchRiskGetPolicyStatus, queryKeyRiskGetPolicyStatus, type RiskGetPolicyStatusQueryData, };
export type RiskGetPolicyStatusQueryError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getRiskPolicyStatus risk
 *
 * @remarks
 * Get the analysis status of a risk policy including progress and workflow state.
 */
export declare function useRiskGetPolicyStatus(request: operations.GetRiskPolicyStatusRequest, security?: operations.GetRiskPolicyStatusSecurity | undefined, options?: QueryHookOptions<RiskGetPolicyStatusQueryData, RiskGetPolicyStatusQueryError>): UseQueryResult<RiskGetPolicyStatusQueryData, RiskGetPolicyStatusQueryError>;
/**
 * getRiskPolicyStatus risk
 *
 * @remarks
 * Get the analysis status of a risk policy including progress and workflow state.
 */
export declare function useRiskGetPolicyStatusSuspense(request: operations.GetRiskPolicyStatusRequest, security?: operations.GetRiskPolicyStatusSecurity | undefined, options?: SuspenseQueryHookOptions<RiskGetPolicyStatusQueryData, RiskGetPolicyStatusQueryError>): UseSuspenseQueryResult<RiskGetPolicyStatusQueryData, RiskGetPolicyStatusQueryError>;
export declare function setRiskGetPolicyStatusData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: RiskGetPolicyStatusQueryData): RiskGetPolicyStatusQueryData | undefined;
export declare function invalidateRiskGetPolicyStatus(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllRiskGetPolicyStatus(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=riskGetPolicyStatus.d.ts.map