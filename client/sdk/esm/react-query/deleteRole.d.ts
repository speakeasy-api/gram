import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteRoleRequest, DeleteRoleSecurity } from "../models/operations/deleterole.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteRoleMutationVariables = {
    request: DeleteRoleRequest;
    security?: DeleteRoleSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteRoleMutationData = void;
export type DeleteRoleMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteRole access
 *
 * @remarks
 * Delete a custom role (system roles cannot be deleted).
 */
export declare function useDeleteRoleMutation(options?: MutationHookOptions<DeleteRoleMutationData, DeleteRoleMutationError, DeleteRoleMutationVariables>): UseMutationResult<DeleteRoleMutationData, DeleteRoleMutationError, DeleteRoleMutationVariables>;
export declare function mutationKeyDeleteRole(): MutationKey;
export declare function buildDeleteRoleMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteRoleMutationVariables) => Promise<DeleteRoleMutationData>;
};
//# sourceMappingURL=deleteRole.d.ts.map