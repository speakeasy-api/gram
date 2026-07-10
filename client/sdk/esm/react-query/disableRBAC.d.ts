import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DisableRBACRequest, DisableRBACSecurity } from "../models/operations/disablerbac.js";
import { MutationHookOptions } from "./_types.js";
export type DisableRBACMutationVariables = {
    request?: DisableRBACRequest | undefined;
    security?: DisableRBACSecurity | undefined;
    options?: RequestOptions;
};
export type DisableRBACMutationData = void;
export type DisableRBACMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * disableRBAC access
 *
 * @remarks
 * Disable RBAC enforcement for the current organization.
 */
export declare function useDisableRBACMutation(options?: MutationHookOptions<DisableRBACMutationData, DisableRBACMutationError, DisableRBACMutationVariables>): UseMutationResult<DisableRBACMutationData, DisableRBACMutationError, DisableRBACMutationVariables>;
export declare function mutationKeyDisableRBAC(): MutationKey;
export declare function buildDisableRBACMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DisableRBACMutationVariables) => Promise<DisableRBACMutationData>;
};
//# sourceMappingURL=disableRBAC.d.ts.map