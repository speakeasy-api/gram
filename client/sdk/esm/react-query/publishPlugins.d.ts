import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PublishPluginsResult } from "../models/components/publishpluginsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { PublishPluginsRequest, PublishPluginsSecurity } from "../models/operations/publishplugins.js";
import { MutationHookOptions } from "./_types.js";
export type PublishPluginsMutationVariables = {
    request: PublishPluginsRequest;
    security?: PublishPluginsSecurity | undefined;
    options?: RequestOptions;
};
export type PublishPluginsMutationData = PublishPluginsResult;
export type PublishPluginsMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * publishPlugins plugins
 *
 * @remarks
 * Generate and publish all plugin packages to a GitHub repository.
 */
export declare function usePublishPluginsMutation(options?: MutationHookOptions<PublishPluginsMutationData, PublishPluginsMutationError, PublishPluginsMutationVariables>): UseMutationResult<PublishPluginsMutationData, PublishPluginsMutationError, PublishPluginsMutationVariables>;
export declare function mutationKeyPublishPlugins(): MutationKey;
export declare function buildPublishPluginsMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: PublishPluginsMutationVariables) => Promise<PublishPluginsMutationData>;
};
//# sourceMappingURL=publishPlugins.d.ts.map