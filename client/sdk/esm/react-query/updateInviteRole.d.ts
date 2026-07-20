import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OrganizationInvitation } from "../models/components/organizationinvitation.js";
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
  UpdateInviteRoleRequest,
  UpdateInviteRoleSecurity,
} from "../models/operations/updateinviterole.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateInviteRoleMutationVariables = {
  request: UpdateInviteRoleRequest;
  security?: UpdateInviteRoleSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateInviteRoleMutationData = OrganizationInvitation;
export type UpdateInviteRoleMutationError =
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
 * updateInviteRole organizations
 *
 * @remarks
 * Change the role assigned to a pending WorkOS invitation.
 */
export declare function useUpdateInviteRoleMutation(
  options?: MutationHookOptions<
    UpdateInviteRoleMutationData,
    UpdateInviteRoleMutationError,
    UpdateInviteRoleMutationVariables
  >,
): UseMutationResult<
  UpdateInviteRoleMutationData,
  UpdateInviteRoleMutationError,
  UpdateInviteRoleMutationVariables
>;
export declare function mutationKeyUpdateInviteRole(): MutationKey;
export declare function buildUpdateInviteRoleMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateInviteRoleMutationVariables,
  ) => Promise<UpdateInviteRoleMutationData>;
};
//# sourceMappingURL=updateInviteRole.d.ts.map
