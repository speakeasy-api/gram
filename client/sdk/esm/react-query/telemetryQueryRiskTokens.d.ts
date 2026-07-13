import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { QueryRiskTokensRequest, QueryRiskTokensSecurity } from "../models/operations/queryrisktokens.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildTelemetryQueryRiskTokensQuery, prefetchTelemetryQueryRiskTokens, queryKeyTelemetryQueryRiskTokens, TelemetryQueryRiskTokensQueryData } from "./telemetryQueryRiskTokens.core.js";
export { buildTelemetryQueryRiskTokensQuery, prefetchTelemetryQueryRiskTokens, queryKeyTelemetryQueryRiskTokens, type TelemetryQueryRiskTokensQueryData, };
export type TelemetryQueryRiskTokensQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * queryRiskTokens telemetry
 *
 * @remarks
 * Org-scoped daily token usage split by risk involvement: tokens from sessions with at least one active risk finding in the window versus all session tokens. Powers the token-usage panel's risk breakdown on the costs page.
 */
export declare function useTelemetryQueryRiskTokens(request: QueryRiskTokensRequest, security?: QueryRiskTokensSecurity | undefined, options?: QueryHookOptions<TelemetryQueryRiskTokensQueryData, TelemetryQueryRiskTokensQueryError>): UseQueryResult<TelemetryQueryRiskTokensQueryData, TelemetryQueryRiskTokensQueryError>;
/**
 * queryRiskTokens telemetry
 *
 * @remarks
 * Org-scoped daily token usage split by risk involvement: tokens from sessions with at least one active risk finding in the window versus all session tokens. Powers the token-usage panel's risk breakdown on the costs page.
 */
export declare function useTelemetryQueryRiskTokensSuspense(request: QueryRiskTokensRequest, security?: QueryRiskTokensSecurity | undefined, options?: SuspenseQueryHookOptions<TelemetryQueryRiskTokensQueryData, TelemetryQueryRiskTokensQueryError>): UseSuspenseQueryResult<TelemetryQueryRiskTokensQueryData, TelemetryQueryRiskTokensQueryError>;
export declare function setTelemetryQueryRiskTokensData(client: QueryClient, queryKeyBase: [parameters: {
    gramSession?: string | undefined;
}], data: TelemetryQueryRiskTokensQueryData): TelemetryQueryRiskTokensQueryData | undefined;
export declare function invalidateTelemetryQueryRiskTokens(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllTelemetryQueryRiskTokens(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=telemetryQueryRiskTokens.d.ts.map