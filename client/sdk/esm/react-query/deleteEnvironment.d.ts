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
  DeleteEnvironmentRequest,
  DeleteEnvironmentSecurity,
} from "../models/operations/deleteenvironment.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteEnvironmentMutationVariables = {
  request: DeleteEnvironmentRequest;
  security?: DeleteEnvironmentSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteEnvironmentMutationData = void;
export type DeleteEnvironmentMutationError =
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
 * deleteEnvironment environments
 *
 * @remarks
 * Delete an environment
 */
export declare function useDeleteEnvironmentMutation(
  options?: MutationHookOptions<
    DeleteEnvironmentMutationData,
    DeleteEnvironmentMutationError,
    DeleteEnvironmentMutationVariables
  >,
): UseMutationResult<
  DeleteEnvironmentMutationData,
  DeleteEnvironmentMutationError,
  DeleteEnvironmentMutationVariables
>;
export declare function mutationKeyDeleteEnvironment(): MutationKey;
export declare function buildDeleteEnvironmentMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteEnvironmentMutationVariables,
  ) => Promise<DeleteEnvironmentMutationData>;
};
//# sourceMappingURL=deleteEnvironment.d.ts.map
