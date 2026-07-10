import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Role } from "../models/components/role.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateRoleRequest, CreateRoleSecurity } from "../models/operations/createrole.js";
import { MutationHookOptions } from "./_types.js";
export type CreateRoleMutationVariables = {
    request: CreateRoleRequest;
    security?: CreateRoleSecurity | undefined;
    options?: RequestOptions;
};
export type CreateRoleMutationData = Role;
export type CreateRoleMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createRole access
 *
 * @remarks
 * Create a new custom role.
 */
export declare function useCreateRoleMutation(options?: MutationHookOptions<CreateRoleMutationData, CreateRoleMutationError, CreateRoleMutationVariables>): UseMutationResult<CreateRoleMutationData, CreateRoleMutationError, CreateRoleMutationVariables>;
export declare function mutationKeyCreateRole(): MutationKey;
export declare function buildCreateRoleMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateRoleMutationVariables) => Promise<CreateRoleMutationData>;
};
//# sourceMappingURL=createRole.d.ts.map