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
  CreateEnvironmentRequest,
  CreateEnvironmentSecurity,
} from "../models/operations/createenvironment.js";
import { MutationHookOptions } from "./_types.js";
export type CreateEnvironmentMutationVariables = {
  request: CreateEnvironmentRequest;
  security?: CreateEnvironmentSecurity | undefined;
  options?: RequestOptions;
};
export type CreateEnvironmentMutationData = Environment;
export type CreateEnvironmentMutationError =
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
 * createEnvironment environments
 *
 * @remarks
 * Create a new environment
 */
export declare function useCreateEnvironmentMutation(
  options?: MutationHookOptions<
    CreateEnvironmentMutationData,
    CreateEnvironmentMutationError,
    CreateEnvironmentMutationVariables
  >,
): UseMutationResult<
  CreateEnvironmentMutationData,
  CreateEnvironmentMutationError,
  CreateEnvironmentMutationVariables
>;
export declare function mutationKeyCreateEnvironment(): MutationKey;
export declare function buildCreateEnvironmentMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateEnvironmentMutationVariables,
  ) => Promise<CreateEnvironmentMutationData>;
};
//# sourceMappingURL=createEnvironment.d.ts.map
