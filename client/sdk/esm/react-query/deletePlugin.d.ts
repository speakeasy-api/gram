import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeletePluginRequest, DeletePluginSecurity } from "../models/operations/deleteplugin.js";
import { MutationHookOptions } from "./_types.js";
export type DeletePluginMutationVariables = {
    request: DeletePluginRequest;
    security?: DeletePluginSecurity | undefined;
    options?: RequestOptions;
};
export type DeletePluginMutationData = void;
export type DeletePluginMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deletePlugin plugins
 *
 * @remarks
 * Delete a plugin.
 */
export declare function useDeletePluginMutation(options?: MutationHookOptions<DeletePluginMutationData, DeletePluginMutationError, DeletePluginMutationVariables>): UseMutationResult<DeletePluginMutationData, DeletePluginMutationError, DeletePluginMutationVariables>;
export declare function mutationKeyDeletePlugin(): MutationKey;
export declare function buildDeletePluginMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeletePluginMutationVariables) => Promise<DeletePluginMutationData>;
};
//# sourceMappingURL=deletePlugin.d.ts.map