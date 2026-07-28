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
  RevokeOrganizationRemoteSessionRequest,
  RevokeOrganizationRemoteSessionSecurity,
} from "../models/operations/revokeorganizationremotesession.js";
import { MutationHookOptions } from "./_types.js";
export type RevokeOrganizationRemoteSessionMutationVariables = {
  request: RevokeOrganizationRemoteSessionRequest;
  security?: RevokeOrganizationRemoteSessionSecurity | undefined;
  options?: RequestOptions;
};
export type RevokeOrganizationRemoteSessionMutationData = void;
export type RevokeOrganizationRemoteSessionMutationError =
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
 * revokeSession organizationRemoteSessions
 *
 * @remarks
 * Revoke (soft-delete) a single remote_session in the caller's organization. Requires org:admin.
 */
export declare function useRevokeOrganizationRemoteSessionMutation(
  options?: MutationHookOptions<
    RevokeOrganizationRemoteSessionMutationData,
    RevokeOrganizationRemoteSessionMutationError,
    RevokeOrganizationRemoteSessionMutationVariables
  >,
): UseMutationResult<
  RevokeOrganizationRemoteSessionMutationData,
  RevokeOrganizationRemoteSessionMutationError,
  RevokeOrganizationRemoteSessionMutationVariables
>;
export declare function mutationKeyRevokeOrganizationRemoteSession(): MutationKey;
export declare function buildRevokeOrganizationRemoteSessionMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RevokeOrganizationRemoteSessionMutationVariables,
  ) => Promise<RevokeOrganizationRemoteSessionMutationData>;
};
//# sourceMappingURL=revokeOrganizationRemoteSession.d.ts.map
