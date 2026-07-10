import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Environment } from "../models/components/environment.js";
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
  UpdateEnvironmentRequest,
  UpdateEnvironmentSecurity,
} from "../models/operations/updateenvironment.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateEnvironmentMutationVariables = {
  request: UpdateEnvironmentRequest;
  security?: UpdateEnvironmentSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateEnvironmentMutationData = Environment;
export type UpdateEnvironmentMutationError =
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
 * updateEnvironment environments
 *
 * @remarks
 * Update an environment
 */
export declare function useUpdateEnvironmentMutation(
  options?: MutationHookOptions<
    UpdateEnvironmentMutationData,
    UpdateEnvironmentMutationError,
    UpdateEnvironmentMutationVariables
  >,
): UseMutationResult<
  UpdateEnvironmentMutationData,
  UpdateEnvironmentMutationError,
  UpdateEnvironmentMutationVariables
>;
export declare function mutationKeyUpdateEnvironment(): MutationKey;
export declare function buildUpdateEnvironmentMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateEnvironmentMutationVariables,
  ) => Promise<UpdateEnvironmentMutationData>;
};
//# sourceMappingURL=updateEnvironment.d.ts.map
