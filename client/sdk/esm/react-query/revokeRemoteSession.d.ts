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
  RevokeRemoteSessionRequest,
  RevokeRemoteSessionSecurity,
} from "../models/operations/revokeremotesession.js";
import { MutationHookOptions } from "./_types.js";
export type RevokeRemoteSessionMutationVariables = {
  request: RevokeRemoteSessionRequest;
  security?: RevokeRemoteSessionSecurity | undefined;
  options?: RequestOptions;
};
export type RevokeRemoteSessionMutationData = void;
export type RevokeRemoteSessionMutationError =
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
 * revokeRemoteSession remoteSessions
 *
 * @remarks
 * Drop a remote_session row. The next /mcp call by that principal triggers a fresh authn challenge.
 */
export declare function useRevokeRemoteSessionMutation(
  options?: MutationHookOptions<
    RevokeRemoteSessionMutationData,
    RevokeRemoteSessionMutationError,
    RevokeRemoteSessionMutationVariables
  >,
): UseMutationResult<
  RevokeRemoteSessionMutationData,
  RevokeRemoteSessionMutationError,
  RevokeRemoteSessionMutationVariables
>;
export declare function mutationKeyRevokeRemoteSession(): MutationKey;
export declare function buildRevokeRemoteSessionMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RevokeRemoteSessionMutationVariables,
  ) => Promise<RevokeRemoteSessionMutationData>;
};
//# sourceMappingURL=revokeRemoteSession.d.ts.map
