import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PluginServer } from "../models/components/pluginserver.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { AddPluginServerRequest, AddPluginServerSecurity } from "../models/operations/addpluginserver.js";
import { MutationHookOptions } from "./_types.js";
export type AddPluginServerMutationVariables = {
    request: AddPluginServerRequest;
    security?: AddPluginServerSecurity | undefined;
    options?: RequestOptions;
};
export type AddPluginServerMutationData = PluginServer;
export type AddPluginServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * addPluginServer plugins
 *
 * @remarks
 * Add an MCP server to a plugin.
 */
export declare function useAddPluginServerMutation(options?: MutationHookOptions<AddPluginServerMutationData, AddPluginServerMutationError, AddPluginServerMutationVariables>): UseMutationResult<AddPluginServerMutationData, AddPluginServerMutationError, AddPluginServerMutationVariables>;
export declare function mutationKeyAddPluginServer(): MutationKey;
export declare function buildAddPluginServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: AddPluginServerMutationVariables) => Promise<AddPluginServerMutationData>;
};
//# sourceMappingURL=addPluginServer.d.ts.map