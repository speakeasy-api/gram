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
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type CancelTeamInviteMutationVariables = {
  request: operations.CancelTeamInviteRequest;
  security?: operations.CancelTeamInviteSecurity | undefined;
  options?: RequestOptions;
};
export type CancelTeamInviteMutationData = void;
export type CancelTeamInviteMutationError =
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
 * cancelInvite teams
 *
 * @remarks
 * Cancel a pending invite.
 */
export declare function useCancelTeamInviteMutation(
  options?: MutationHookOptions<
    CancelTeamInviteMutationData,
    CancelTeamInviteMutationError,
    CancelTeamInviteMutationVariables
  >,
): UseMutationResult<
  CancelTeamInviteMutationData,
  CancelTeamInviteMutationError,
  CancelTeamInviteMutationVariables
>;
export declare function mutationKeyCancelTeamInvite(): MutationKey;
export declare function buildCancelTeamInviteMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CancelTeamInviteMutationVariables,
  ) => Promise<CancelTeamInviteMutationData>;
};
//# sourceMappingURL=cancelTeamInvite.d.ts.map
