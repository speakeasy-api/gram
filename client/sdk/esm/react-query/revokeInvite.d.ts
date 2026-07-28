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
  RevokeInviteRequest,
  RevokeInviteSecurity,
} from "../models/operations/revokeinvite.js";
import { MutationHookOptions } from "./_types.js";
export type RevokeInviteMutationVariables = {
  request: RevokeInviteRequest;
  security?: RevokeInviteSecurity | undefined;
  options?: RequestOptions;
};
export type RevokeInviteMutationData = void;
export type RevokeInviteMutationError =
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
 * revokeInvite organizations
 *
 * @remarks
 * Revoke a pending WorkOS invitation.
 */
export declare function useRevokeInviteMutation(
  options?: MutationHookOptions<
    RevokeInviteMutationData,
    RevokeInviteMutationError,
    RevokeInviteMutationVariables
  >,
): UseMutationResult<
  RevokeInviteMutationData,
  RevokeInviteMutationError,
  RevokeInviteMutationVariables
>;
export declare function mutationKeyRevokeInvite(): MutationKey;
export declare function buildRevokeInviteMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RevokeInviteMutationVariables,
  ) => Promise<RevokeInviteMutationData>;
};
//# sourceMappingURL=revokeInvite.d.ts.map
