import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
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
export type UpdateSecurityVariableDisplayNameMutationVariables = {
  request: operations.UpdateSecurityVariableDisplayNameRequest;
  security?: operations.UpdateSecurityVariableDisplayNameSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateSecurityVariableDisplayNameMutationData =
  components.SecurityVariable;
export type UpdateSecurityVariableDisplayNameMutationError =
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
 * updateSecurityVariableDisplayName toolsets
 *
 * @remarks
 * Update the display name of a security variable for user-friendly presentation
 */
export declare function useUpdateSecurityVariableDisplayNameMutation(
  options?: MutationHookOptions<
    UpdateSecurityVariableDisplayNameMutationData,
    UpdateSecurityVariableDisplayNameMutationError,
    UpdateSecurityVariableDisplayNameMutationVariables
  >,
): UseMutationResult<
  UpdateSecurityVariableDisplayNameMutationData,
  UpdateSecurityVariableDisplayNameMutationError,
  UpdateSecurityVariableDisplayNameMutationVariables
>;
export declare function mutationKeyUpdateSecurityVariableDisplayName(): MutationKey;
export declare function buildUpdateSecurityVariableDisplayNameMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateSecurityVariableDisplayNameMutationVariables,
  ) => Promise<UpdateSecurityVariableDisplayNameMutationData>;
};
//# sourceMappingURL=updateSecurityVariableDisplayName.d.ts.map
