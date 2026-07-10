import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RemovePluginServerRequest, RemovePluginServerSecurity } from "../models/operations/removepluginserver.js";
import { MutationHookOptions } from "./_types.js";
export type RemovePluginServerMutationVariables = {
    request: RemovePluginServerRequest;
    security?: RemovePluginServerSecurity | undefined;
    options?: RequestOptions;
};
export type RemovePluginServerMutationData = void;
export type RemovePluginServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * removePluginServer plugins
 *
 * @remarks
 * Remove a server from a plugin.
 */
export declare function useRemovePluginServerMutation(options?: MutationHookOptions<RemovePluginServerMutationData, RemovePluginServerMutationError, RemovePluginServerMutationVariables>): UseMutationResult<RemovePluginServerMutationData, RemovePluginServerMutationError, RemovePluginServerMutationVariables>;
export declare function mutationKeyRemovePluginServer(): MutationKey;
export declare function buildRemovePluginServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RemovePluginServerMutationVariables) => Promise<RemovePluginServerMutationData>;
};
//# sourceMappingURL=removePluginServer.d.ts.map