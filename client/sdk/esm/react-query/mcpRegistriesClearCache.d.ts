import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ClearMCPRegistryCacheRequest, ClearMCPRegistryCacheSecurity } from "../models/operations/clearmcpregistrycache.js";
import { MutationHookOptions } from "./_types.js";
export type McpRegistriesClearCacheMutationVariables = {
    request: ClearMCPRegistryCacheRequest;
    security?: ClearMCPRegistryCacheSecurity | undefined;
    options?: RequestOptions;
};
export type McpRegistriesClearCacheMutationData = void;
export type McpRegistriesClearCacheMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * clearCache mcpRegistries
 *
 * @remarks
 * Clear the registry cache for a specific registry (admin only)
 */
export declare function useMcpRegistriesClearCacheMutation(options?: MutationHookOptions<McpRegistriesClearCacheMutationData, McpRegistriesClearCacheMutationError, McpRegistriesClearCacheMutationVariables>): UseMutationResult<McpRegistriesClearCacheMutationData, McpRegistriesClearCacheMutationError, McpRegistriesClearCacheMutationVariables>;
export declare function mutationKeyMcpRegistriesClearCache(): MutationKey;
export declare function buildMcpRegistriesClearCacheMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: McpRegistriesClearCacheMutationVariables) => Promise<McpRegistriesClearCacheMutationData>;
};
//# sourceMappingURL=mcpRegistriesClearCache.d.ts.map