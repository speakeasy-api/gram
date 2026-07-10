import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AccessMember } from "../models/components/accessmember.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateMemberRolesRequest, UpdateMemberRolesSecurity } from "../models/operations/updatememberroles.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateMemberRolesMutationVariables = {
    request: UpdateMemberRolesRequest;
    security?: UpdateMemberRolesSecurity | undefined;
    options?: RequestOptions;
};
export type UpdateMemberRolesMutationData = AccessMember;
export type UpdateMemberRolesMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateMemberRoles access
 *
 * @remarks
 * Update a team member's role assignments.
 */
export declare function useUpdateMemberRolesMutation(options?: MutationHookOptions<UpdateMemberRolesMutationData, UpdateMemberRolesMutationError, UpdateMemberRolesMutationVariables>): UseMutationResult<UpdateMemberRolesMutationData, UpdateMemberRolesMutationError, UpdateMemberRolesMutationVariables>;
export declare function mutationKeyUpdateMemberRoles(): MutationKey;
export declare function buildUpdateMemberRolesMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpdateMemberRolesMutationVariables) => Promise<UpdateMemberRolesMutationData>;
};
//# sourceMappingURL=updateMemberRoles.d.ts.map