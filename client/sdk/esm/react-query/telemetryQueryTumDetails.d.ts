import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { QueryTumDetailsRequest, QueryTumDetailsSecurity } from "../models/operations/querytumdetails.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildTelemetryQueryTumDetailsQuery, prefetchTelemetryQueryTumDetails, queryKeyTelemetryQueryTumDetails, TelemetryQueryTumDetailsQueryData } from "./telemetryQueryTumDetails.core.js";
export { buildTelemetryQueryTumDetailsQuery, prefetchTelemetryQueryTumDetails, queryKeyTelemetryQueryTumDetails, type TelemetryQueryTumDetailsQueryData, };
export type TelemetryQueryTumDetailsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * queryTumDetails telemetry
 *
 * @remarks
 * Org-scoped daily usage details for the billing page's metrics table, computed in one pass: token type sums, session/tool-call/active-user counts, attribution slices (MCP tools, skills, unattributed users), and message-level stats (tokens in messages with active risk findings, tokens in tool-call messages).
 */
export declare function useTelemetryQueryTumDetails(request: QueryTumDetailsRequest, security?: QueryTumDetailsSecurity | undefined, options?: QueryHookOptions<TelemetryQueryTumDetailsQueryData, TelemetryQueryTumDetailsQueryError>): UseQueryResult<TelemetryQueryTumDetailsQueryData, TelemetryQueryTumDetailsQueryError>;
/**
 * queryTumDetails telemetry
 *
 * @remarks
 * Org-scoped daily usage details for the billing page's metrics table, computed in one pass: token type sums, session/tool-call/active-user counts, attribution slices (MCP tools, skills, unattributed users), and message-level stats (tokens in messages with active risk findings, tokens in tool-call messages).
 */
export declare function useTelemetryQueryTumDetailsSuspense(request: QueryTumDetailsRequest, security?: QueryTumDetailsSecurity | undefined, options?: SuspenseQueryHookOptions<TelemetryQueryTumDetailsQueryData, TelemetryQueryTumDetailsQueryError>): UseSuspenseQueryResult<TelemetryQueryTumDetailsQueryData, TelemetryQueryTumDetailsQueryError>;
export declare function setTelemetryQueryTumDetailsData(client: QueryClient, queryKeyBase: [parameters: {
    gramSession?: string | undefined;
}], data: TelemetryQueryTumDetailsQueryData): TelemetryQueryTumDetailsQueryData | undefined;
export declare function invalidateTelemetryQueryTumDetails(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllTelemetryQueryTumDetails(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=telemetryQueryTumDetails.d.ts.map