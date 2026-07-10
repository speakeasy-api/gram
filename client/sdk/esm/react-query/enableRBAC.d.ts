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
  EnableRBACRequest,
  EnableRBACSecurity,
} from "../models/operations/enablerbac.js";
import { MutationHookOptions } from "./_types.js";
export type EnableRBACMutationVariables = {
  request?: EnableRBACRequest | undefined;
  security?: EnableRBACSecurity | undefined;
  options?: RequestOptions;
};
export type EnableRBACMutationData = void;
export type EnableRBACMutationError =
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
 * enableRBAC access
 *
 * @remarks
 * Enable RBAC for the current organization. Seeds default grants for system roles.
 */
export declare function useEnableRBACMutation(
  options?: MutationHookOptions<
    EnableRBACMutationData,
    EnableRBACMutationError,
    EnableRBACMutationVariables
  >,
): UseMutationResult<
  EnableRBACMutationData,
  EnableRBACMutationError,
  EnableRBACMutationVariables
>;
export declare function mutationKeyEnableRBAC(): MutationKey;
export declare function buildEnableRBACMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: EnableRBACMutationVariables,
  ) => Promise<EnableRBACMutationData>;
};
//# sourceMappingURL=enableRBAC.d.ts.map
