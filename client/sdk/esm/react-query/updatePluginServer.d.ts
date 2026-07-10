import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PluginServer } from "../models/components/pluginserver.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  UpdatePluginServerRequest,
  UpdatePluginServerSecurity,
} from "../models/operations/updatepluginserver.js";
import { MutationHookOptions } from "./_types.js";
export type UpdatePluginServerMutationVariables = {
  request: UpdatePluginServerRequest;
  security?: UpdatePluginServerSecurity | undefined;
  options?: RequestOptions;
};
export type UpdatePluginServerMutationData = PluginServer;
export type UpdatePluginServerMutationError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * updatePluginServer plugins
 *
 * @remarks
 * Update a server's configuration within a plugin.
 */
export declare function useUpdatePluginServerMutation(
  options?: MutationHookOptions<
    UpdatePluginServerMutationData,
    UpdatePluginServerMutationError,
    UpdatePluginServerMutationVariables
  >,
): UseMutationResult<
  UpdatePluginServerMutationData,
  UpdatePluginServerMutationError,
  UpdatePluginServerMutationVariables
>;
export declare function mutationKeyUpdatePluginServer(): MutationKey;
export declare function buildUpdatePluginServerMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdatePluginServerMutationVariables,
  ) => Promise<UpdatePluginServerMutationData>;
};
//# sourceMappingURL=updatePluginServer.d.ts.map
