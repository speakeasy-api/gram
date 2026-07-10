import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetPluginRequest, GetPluginSecurity } from "../models/operations/getplugin.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildPluginQuery, PluginQueryData, prefetchPlugin, queryKeyPlugin } from "./plugin.core.js";
export { buildPluginQuery, type PluginQueryData, prefetchPlugin, queryKeyPlugin, };
export type PluginQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getPlugin plugins
 *
 * @remarks
 * Get a plugin with its servers and assignments.
 */
export declare function usePlugin(request: GetPluginRequest, security?: GetPluginSecurity | undefined, options?: QueryHookOptions<PluginQueryData, PluginQueryError>): UseQueryResult<PluginQueryData, PluginQueryError>;
/**
 * getPlugin plugins
 *
 * @remarks
 * Get a plugin with its servers and assignments.
 */
export declare function usePluginSuspense(request: GetPluginRequest, security?: GetPluginSecurity | undefined, options?: SuspenseQueryHookOptions<PluginQueryData, PluginQueryError>): UseSuspenseQueryResult<PluginQueryData, PluginQueryError>;
export declare function setPluginData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: PluginQueryData): PluginQueryData | undefined;
export declare function invalidatePlugin(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllPlugin(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=plugin.d.ts.map