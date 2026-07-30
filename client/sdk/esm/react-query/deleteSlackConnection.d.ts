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
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteSlackConnectionMutationVariables = {
  request?: operations.DeleteSlackConnectionRequest | undefined;
  security?: operations.DeleteSlackConnectionSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteSlackConnectionMutationData = void;
export type DeleteSlackConnectionMutationError =
  | errors.ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * deleteSlackConnection slack
 *
 * @remarks
 * delete slack connection for an organization and project.
 */
export declare function useDeleteSlackConnectionMutation(
  options?: MutationHookOptions<
    DeleteSlackConnectionMutationData,
    DeleteSlackConnectionMutationError,
    DeleteSlackConnectionMutationVariables
  >,
): UseMutationResult<
  DeleteSlackConnectionMutationData,
  DeleteSlackConnectionMutationError,
  DeleteSlackConnectionMutationVariables
>;
export declare function mutationKeyDeleteSlackConnection(): MutationKey;
export declare function buildDeleteSlackConnectionMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteSlackConnectionMutationVariables,
  ) => Promise<DeleteSlackConnectionMutationData>;
};
//# sourceMappingURL=deleteSlackConnection.d.ts.map
