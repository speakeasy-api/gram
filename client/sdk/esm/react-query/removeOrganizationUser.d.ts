import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RemoveOrganizationUserRequest, RemoveOrganizationUserSecurity } from "../models/operations/removeorganizationuser.js";
import { MutationHookOptions } from "./_types.js";
export type RemoveOrganizationUserMutationVariables = {
    request: RemoveOrganizationUserRequest;
    security?: RemoveOrganizationUserSecurity | undefined;
    options?: RequestOptions;
};
export type RemoveOrganizationUserMutationData = void;
export type RemoveOrganizationUserMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * removeUser organizations
 *
 * @remarks
 * Remove a user from the active organization in Gram and delete their WorkOS organization membership.
 */
export declare function useRemoveOrganizationUserMutation(options?: MutationHookOptions<RemoveOrganizationUserMutationData, RemoveOrganizationUserMutationError, RemoveOrganizationUserMutationVariables>): UseMutationResult<RemoveOrganizationUserMutationData, RemoveOrganizationUserMutationError, RemoveOrganizationUserMutationVariables>;
export declare function mutationKeyRemoveOrganizationUser(): MutationKey;
export declare function buildRemoveOrganizationUserMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RemoveOrganizationUserMutationVariables) => Promise<RemoveOrganizationUserMutationData>;
};
//# sourceMappingURL=removeOrganizationUser.d.ts.map