import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  DeleteToolsetEnvironmentLinkRequest,
  DeleteToolsetEnvironmentLinkSecurity,
} from "../models/operations/deletetoolsetenvironmentlink.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteToolsetEnvironmentLinkMutationVariables = {
  request: DeleteToolsetEnvironmentLinkRequest;
  security?: DeleteToolsetEnvironmentLinkSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteToolsetEnvironmentLinkMutationData = void;
export type DeleteToolsetEnvironmentLinkMutationError =
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
 * deleteToolsetEnvironmentLink environments
 *
 * @remarks
 * Delete a link between a toolset and an environment
 */
export declare function useDeleteToolsetEnvironmentLinkMutation(
  options?: MutationHookOptions<
    DeleteToolsetEnvironmentLinkMutationData,
    DeleteToolsetEnvironmentLinkMutationError,
    DeleteToolsetEnvironmentLinkMutationVariables
  >,
): UseMutationResult<
  DeleteToolsetEnvironmentLinkMutationData,
  DeleteToolsetEnvironmentLinkMutationError,
  DeleteToolsetEnvironmentLinkMutationVariables
>;
export declare function mutationKeyDeleteToolsetEnvironmentLink(): MutationKey;
export declare function buildDeleteToolsetEnvironmentLinkMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteToolsetEnvironmentLinkMutationVariables,
  ) => Promise<DeleteToolsetEnvironmentLinkMutationData>;
};
//# sourceMappingURL=deleteToolsetEnvironmentLink.d.ts.map
