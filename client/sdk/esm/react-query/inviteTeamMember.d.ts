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
export type InviteTeamMemberMutationVariables = {
  request: operations.InviteTeamMemberRequest;
  security?: operations.InviteTeamMemberSecurity | undefined;
  options?: RequestOptions;
};
export type InviteTeamMemberMutationData = components.InviteMemberResult;
export type InviteTeamMemberMutationError =
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
 * inviteMember teams
 *
 * @remarks
 * Invite a new member to the organization.
 */
export declare function useInviteTeamMemberMutation(
  options?: MutationHookOptions<
    InviteTeamMemberMutationData,
    InviteTeamMemberMutationError,
    InviteTeamMemberMutationVariables
  >,
): UseMutationResult<
  InviteTeamMemberMutationData,
  InviteTeamMemberMutationError,
  InviteTeamMemberMutationVariables
>;
export declare function mutationKeyInviteTeamMember(): MutationKey;
export declare function buildInviteTeamMemberMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: InviteTeamMemberMutationVariables,
  ) => Promise<InviteTeamMemberMutationData>;
};
//# sourceMappingURL=inviteTeamMember.d.ts.map
