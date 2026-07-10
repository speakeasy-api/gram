import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetRiskUserBreakdownRequest, GetRiskUserBreakdownSecurity } from "../models/operations/getriskuserbreakdown.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildRiskUserBreakdownQuery, prefetchRiskUserBreakdown, queryKeyRiskUserBreakdown, RiskUserBreakdownQueryData } from "./riskUserBreakdown.core.js";
export { buildRiskUserBreakdownQuery, prefetchRiskUserBreakdown, queryKeyRiskUserBreakdown, type RiskUserBreakdownQueryData, };
export type RiskUserBreakdownQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getRiskUserBreakdown risk
 *
 * @remarks
 * Per-user breakdowns of findings by category and by rule_id within a time window. Powers the user drill-down on /risk-overview.
 */
export declare function useRiskUserBreakdown(request: GetRiskUserBreakdownRequest, security?: GetRiskUserBreakdownSecurity | undefined, options?: QueryHookOptions<RiskUserBreakdownQueryData, RiskUserBreakdownQueryError>): UseQueryResult<RiskUserBreakdownQueryData, RiskUserBreakdownQueryError>;
/**
 * getRiskUserBreakdown risk
 *
 * @remarks
 * Per-user breakdowns of findings by category and by rule_id within a time window. Powers the user drill-down on /risk-overview.
 */
export declare function useRiskUserBreakdownSuspense(request: GetRiskUserBreakdownRequest, security?: GetRiskUserBreakdownSecurity | undefined, options?: SuspenseQueryHookOptions<RiskUserBreakdownQueryData, RiskUserBreakdownQueryError>): UseSuspenseQueryResult<RiskUserBreakdownQueryData, RiskUserBreakdownQueryError>;
export declare function setRiskUserBreakdownData(client: QueryClient, queryKeyBase: [
    parameters: {
        externalUserId: string;
        from?: Date | undefined;
        to?: Date | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: RiskUserBreakdownQueryData): RiskUserBreakdownQueryData | undefined;
export declare function invalidateRiskUserBreakdown(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        externalUserId: string;
        from?: Date | undefined;
        to?: Date | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllRiskUserBreakdown(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=riskUserBreakdown.d.ts.map