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
  RevokeUserSessionRequest,
  RevokeUserSessionSecurity,
} from "../models/operations/revokeusersession.js";
import { MutationHookOptions } from "./_types.js";
export type RevokeUserSessionMutationVariables = {
  request: RevokeUserSessionRequest;
  security?: RevokeUserSessionSecurity | undefined;
  options?: RequestOptions;
};
export type RevokeUserSessionMutationData = void;
export type RevokeUserSessionMutationError =
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
 * revokeUserSession userSessions
 *
 * @remarks
 * Push the session's jti into the revocation cache and soft-delete the row.
 */
export declare function useRevokeUserSessionMutation(
  options?: MutationHookOptions<
    RevokeUserSessionMutationData,
    RevokeUserSessionMutationError,
    RevokeUserSessionMutationVariables
  >,
): UseMutationResult<
  RevokeUserSessionMutationData,
  RevokeUserSessionMutationError,
  RevokeUserSessionMutationVariables
>;
export declare function mutationKeyRevokeUserSession(): MutationKey;
export declare function buildRevokeUserSessionMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RevokeUserSessionMutationVariables,
  ) => Promise<RevokeUserSessionMutationData>;
};
//# sourceMappingURL=revokeUserSession.d.ts.map
