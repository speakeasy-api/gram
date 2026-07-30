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
export type ResendTeamInviteMutationVariables = {
  request: operations.ResendTeamInviteRequest;
  security?: operations.ResendTeamInviteSecurity | undefined;
  options?: RequestOptions;
};
export type ResendTeamInviteMutationData = components.ResendInviteResult;
export type ResendTeamInviteMutationError =
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
 * resendInvite teams
 *
 * @remarks
 * Resend an invite email.
 */
export declare function useResendTeamInviteMutation(
  options?: MutationHookOptions<
    ResendTeamInviteMutationData,
    ResendTeamInviteMutationError,
    ResendTeamInviteMutationVariables
  >,
): UseMutationResult<
  ResendTeamInviteMutationData,
  ResendTeamInviteMutationError,
  ResendTeamInviteMutationVariables
>;
export declare function mutationKeyResendTeamInvite(): MutationKey;
export declare function buildResendTeamInviteMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: ResendTeamInviteMutationVariables,
  ) => Promise<ResendTeamInviteMutationData>;
};
//# sourceMappingURL=resendTeamInvite.d.ts.map
