import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetOtelForwardingConfigRequest, GetOtelForwardingConfigSecurity } from "../models/operations/getotelforwardingconfig.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildOtelForwardingConfigQuery, OtelForwardingConfigQueryData, prefetchOtelForwardingConfig, queryKeyOtelForwardingConfig } from "./otelForwardingConfig.core.js";
export { buildOtelForwardingConfigQuery, type OtelForwardingConfigQueryData, prefetchOtelForwardingConfig, queryKeyOtelForwardingConfig, };
export type OtelForwardingConfigQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getConfig otelForwarding
 *
 * @remarks
 * Get the org-wide OTEL forwarding config. Returns an empty config (enabled=false, no URL) when none is set.
 */
export declare function useOtelForwardingConfig(request?: GetOtelForwardingConfigRequest | undefined, security?: GetOtelForwardingConfigSecurity | undefined, options?: QueryHookOptions<OtelForwardingConfigQueryData, OtelForwardingConfigQueryError>): UseQueryResult<OtelForwardingConfigQueryData, OtelForwardingConfigQueryError>;
/**
 * getConfig otelForwarding
 *
 * @remarks
 * Get the org-wide OTEL forwarding config. Returns an empty config (enabled=false, no URL) when none is set.
 */
export declare function useOtelForwardingConfigSuspense(request?: GetOtelForwardingConfigRequest | undefined, security?: GetOtelForwardingConfigSecurity | undefined, options?: SuspenseQueryHookOptions<OtelForwardingConfigQueryData, OtelForwardingConfigQueryError>): UseSuspenseQueryResult<OtelForwardingConfigQueryData, OtelForwardingConfigQueryError>;
export declare function setOtelForwardingConfigData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
], data: OtelForwardingConfigQueryData): OtelForwardingConfigQueryData | undefined;
export declare function invalidateOtelForwardingConfig(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllOtelForwardingConfig(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=otelForwardingConfig.d.ts.map