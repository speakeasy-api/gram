import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Plugin } from "../models/components/plugin.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdatePluginRequest, UpdatePluginSecurity } from "../models/operations/updateplugin.js";
import { MutationHookOptions } from "./_types.js";
export type UpdatePluginMutationVariables = {
    request: UpdatePluginRequest;
    security?: UpdatePluginSecurity | undefined;
    options?: RequestOptions;
};
export type UpdatePluginMutationData = Plugin;
export type UpdatePluginMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updatePlugin plugins
 *
 * @remarks
 * Update plugin metadata.
 */
export declare function useUpdatePluginMutation(options?: MutationHookOptions<UpdatePluginMutationData, UpdatePluginMutationError, UpdatePluginMutationVariables>): UseMutationResult<UpdatePluginMutationData, UpdatePluginMutationError, UpdatePluginMutationVariables>;
export declare function mutationKeyUpdatePlugin(): MutationKey;
export declare function buildUpdatePluginMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpdatePluginMutationVariables) => Promise<UpdatePluginMutationData>;
};
//# sourceMappingURL=updatePlugin.d.ts.map