import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetCustomDetectionRuleRequest, GetCustomDetectionRuleSecurity } from "../models/operations/getcustomdetectionrule.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildRiskGetCustomDetectionRuleQuery, prefetchRiskGetCustomDetectionRule, queryKeyRiskGetCustomDetectionRule, RiskGetCustomDetectionRuleQueryData } from "./riskGetCustomDetectionRule.core.js";
export { buildRiskGetCustomDetectionRuleQuery, prefetchRiskGetCustomDetectionRule, queryKeyRiskGetCustomDetectionRule, type RiskGetCustomDetectionRuleQueryData, };
export type RiskGetCustomDetectionRuleQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getCustomDetectionRule risk
 *
 * @remarks
 * Get a custom detection rule by ID.
 */
export declare function useRiskGetCustomDetectionRule(request: GetCustomDetectionRuleRequest, security?: GetCustomDetectionRuleSecurity | undefined, options?: QueryHookOptions<RiskGetCustomDetectionRuleQueryData, RiskGetCustomDetectionRuleQueryError>): UseQueryResult<RiskGetCustomDetectionRuleQueryData, RiskGetCustomDetectionRuleQueryError>;
/**
 * getCustomDetectionRule risk
 *
 * @remarks
 * Get a custom detection rule by ID.
 */
export declare function useRiskGetCustomDetectionRuleSuspense(request: GetCustomDetectionRuleRequest, security?: GetCustomDetectionRuleSecurity | undefined, options?: SuspenseQueryHookOptions<RiskGetCustomDetectionRuleQueryData, RiskGetCustomDetectionRuleQueryError>): UseSuspenseQueryResult<RiskGetCustomDetectionRuleQueryData, RiskGetCustomDetectionRuleQueryError>;
export declare function setRiskGetCustomDetectionRuleData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: RiskGetCustomDetectionRuleQueryData): RiskGetCustomDetectionRuleQueryData | undefined;
export declare function invalidateRiskGetCustomDetectionRule(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllRiskGetCustomDetectionRule(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=riskGetCustomDetectionRule.d.ts.map