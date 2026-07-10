import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildOtelForwardingDestinationsQuery, OtelForwardingDestinationsQueryData, prefetchOtelForwardingDestinations, queryKeyOtelForwardingDestinations } from "./otelForwardingDestinations.core.js";
export { buildOtelForwardingDestinationsQuery, type OtelForwardingDestinationsQueryData, prefetchOtelForwardingDestinations, queryKeyOtelForwardingDestinations, };
export type OtelForwardingDestinationsQueryError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listDestinations otelForwarding
 *
 * @remarks
 * List every OTEL forwarding destination configured for the organization.
 */
export declare function useOtelForwardingDestinations(request?: operations.ListOtelForwardingDestinationsRequest | undefined, security?: operations.ListOtelForwardingDestinationsSecurity | undefined, options?: QueryHookOptions<OtelForwardingDestinationsQueryData, OtelForwardingDestinationsQueryError>): UseQueryResult<OtelForwardingDestinationsQueryData, OtelForwardingDestinationsQueryError>;
/**
 * listDestinations otelForwarding
 *
 * @remarks
 * List every OTEL forwarding destination configured for the organization.
 */
export declare function useOtelForwardingDestinationsSuspense(request?: operations.ListOtelForwardingDestinationsRequest | undefined, security?: operations.ListOtelForwardingDestinationsSecurity | undefined, options?: SuspenseQueryHookOptions<OtelForwardingDestinationsQueryData, OtelForwardingDestinationsQueryError>): UseSuspenseQueryResult<OtelForwardingDestinationsQueryData, OtelForwardingDestinationsQueryError>;
export declare function setOtelForwardingDestinationsData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
], data: OtelForwardingDestinationsQueryData): OtelForwardingDestinationsQueryData | undefined;
export declare function invalidateOtelForwardingDestinations(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllOtelForwardingDestinations(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=otelForwardingDestinations.d.ts.map