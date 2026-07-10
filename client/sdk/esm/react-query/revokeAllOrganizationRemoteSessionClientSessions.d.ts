import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RevokeAllRemoteSessionsResult } from "../models/components/revokeallremotesessionsresult.js";
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
  RevokeAllOrganizationRemoteSessionClientSessionsRequest,
  RevokeAllOrganizationRemoteSessionClientSessionsSecurity,
} from "../models/operations/revokeallorganizationremotesessionclientsessions.js";
import { MutationHookOptions } from "./_types.js";
export type RevokeAllOrganizationRemoteSessionClientSessionsMutationVariables =
  {
    request: RevokeAllOrganizationRemoteSessionClientSessionsRequest;
    security?:
      | RevokeAllOrganizationRemoteSessionClientSessionsSecurity
      | undefined;
    options?: RequestOptions;
  };
export type RevokeAllOrganizationRemoteSessionClientSessionsMutationData =
  RevokeAllRemoteSessionsResult;
export type RevokeAllOrganizationRemoteSessionClientSessionsMutationError =
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
 * revokeAllClientSessions organizationRemoteSessions
 *
 * @remarks
 * Revoke (soft-delete) all remote_sessions minted against a remote_session_client in the caller's organization. Requires org:admin.
 */
export declare function useRevokeAllOrganizationRemoteSessionClientSessionsMutation(
  options?: MutationHookOptions<
    RevokeAllOrganizationRemoteSessionClientSessionsMutationData,
    RevokeAllOrganizationRemoteSessionClientSessionsMutationError,
    RevokeAllOrganizationRemoteSessionClientSessionsMutationVariables
  >,
): UseMutationResult<
  RevokeAllOrganizationRemoteSessionClientSessionsMutationData,
  RevokeAllOrganizationRemoteSessionClientSessionsMutationError,
  RevokeAllOrganizationRemoteSessionClientSessionsMutationVariables
>;
export declare function mutationKeyRevokeAllOrganizationRemoteSessionClientSessions(): MutationKey;
export declare function buildRevokeAllOrganizationRemoteSessionClientSessionsMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RevokeAllOrganizationRemoteSessionClientSessionsMutationVariables,
  ) => Promise<RevokeAllOrganizationRemoteSessionClientSessionsMutationData>;
};
//# sourceMappingURL=revokeAllOrganizationRemoteSessionClientSessions.d.ts.map
