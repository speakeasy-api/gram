import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateMemberRoleMutationVariables = {
    request: operations.UpdateMemberRoleRequest;
    security?: operations.UpdateMemberRoleSecurity | undefined;
    options?: RequestOptions;
};
export type UpdateMemberRoleMutationData = components.AccessMember;
export type UpdateMemberRoleMutationError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateMemberRole access
 *
 * @remarks
 * Change a team member's role assignment.
 */
export declare function useUpdateMemberRoleMutation(options?: MutationHookOptions<UpdateMemberRoleMutationData, UpdateMemberRoleMutationError, UpdateMemberRoleMutationVariables>): UseMutationResult<UpdateMemberRoleMutationData, UpdateMemberRoleMutationError, UpdateMemberRoleMutationVariables>;
export declare function mutationKeyUpdateMemberRole(): MutationKey;
export declare function buildUpdateMemberRoleMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpdateMemberRoleMutationVariables) => Promise<UpdateMemberRoleMutationData>;
};
//# sourceMappingURL=updateMemberRole.d.ts.map