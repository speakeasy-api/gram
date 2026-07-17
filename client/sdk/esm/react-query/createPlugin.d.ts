import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Plugin } from "../models/components/plugin.js";
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
  CreatePluginRequest,
  CreatePluginSecurity,
} from "../models/operations/createplugin.js";
import { MutationHookOptions } from "./_types.js";
export type CreatePluginMutationVariables = {
  request: CreatePluginRequest;
  security?: CreatePluginSecurity | undefined;
  options?: RequestOptions;
};
export type CreatePluginMutationData = Plugin;
export type CreatePluginMutationError =
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
 * createPlugin plugins
 *
 * @remarks
 * Create a new plugin.
 */
export declare function useCreatePluginMutation(
  options?: MutationHookOptions<
    CreatePluginMutationData,
    CreatePluginMutationError,
    CreatePluginMutationVariables
  >,
): UseMutationResult<
  CreatePluginMutationData,
  CreatePluginMutationError,
  CreatePluginMutationVariables
>;
export declare function mutationKeyCreatePlugin(): MutationKey;
export declare function buildCreatePluginMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreatePluginMutationVariables,
  ) => Promise<CreatePluginMutationData>;
};
//# sourceMappingURL=createPlugin.d.ts.map
