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
  LogoutRequest,
  LogoutResponse,
  LogoutSecurity,
} from "../models/operations/logout.js";
import { MutationHookOptions } from "./_types.js";
export type LogoutMutationVariables = {
  request?: LogoutRequest | undefined;
  security?: LogoutSecurity | undefined;
  options?: RequestOptions;
};
export type LogoutMutationData = LogoutResponse | undefined;
export type LogoutMutationError =
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
 * logout auth
 *
 * @remarks
 * Logs out the current user by clearing their session.
 */
export declare function useLogoutMutation(
  options?: MutationHookOptions<
    LogoutMutationData,
    LogoutMutationError,
    LogoutMutationVariables
  >,
): UseMutationResult<
  LogoutMutationData,
  LogoutMutationError,
  LogoutMutationVariables
>;
export declare function mutationKeyLogout(): MutationKey;
export declare function buildLogoutMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: LogoutMutationVariables,
  ) => Promise<LogoutMutationData>;
};
//# sourceMappingURL=logout.d.ts.map
