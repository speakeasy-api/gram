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
export type RemoveTeamMemberMutationVariables = {
  request: operations.RemoveTeamMemberRequest;
  security?: operations.RemoveTeamMemberSecurity | undefined;
  options?: RequestOptions;
};
export type RemoveTeamMemberMutationData = void;
export type RemoveTeamMemberMutationError =
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
 * removeMember teams
 *
 * @remarks
 * Remove a member from the organization.
 */
export declare function useRemoveTeamMemberMutation(
  options?: MutationHookOptions<
    RemoveTeamMemberMutationData,
    RemoveTeamMemberMutationError,
    RemoveTeamMemberMutationVariables
  >,
): UseMutationResult<
  RemoveTeamMemberMutationData,
  RemoveTeamMemberMutationError,
  RemoveTeamMemberMutationVariables
>;
export declare function mutationKeyRemoveTeamMember(): MutationKey;
export declare function buildRemoveTeamMemberMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RemoveTeamMemberMutationVariables,
  ) => Promise<RemoveTeamMemberMutationData>;
};
//# sourceMappingURL=removeTeamMember.d.ts.map
