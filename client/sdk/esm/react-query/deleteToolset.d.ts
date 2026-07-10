import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteToolsetRequest, DeleteToolsetSecurity } from "../models/operations/deletetoolset.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteToolsetMutationVariables = {
    request: DeleteToolsetRequest;
    security?: DeleteToolsetSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteToolsetMutationData = void;
export type DeleteToolsetMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteToolset toolsets
 *
 * @remarks
 * Delete a toolset by its ID
 */
export declare function useDeleteToolsetMutation(options?: MutationHookOptions<DeleteToolsetMutationData, DeleteToolsetMutationError, DeleteToolsetMutationVariables>): UseMutationResult<DeleteToolsetMutationData, DeleteToolsetMutationError, DeleteToolsetMutationVariables>;
export declare function mutationKeyDeleteToolset(): MutationKey;
export declare function buildDeleteToolsetMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteToolsetMutationVariables) => Promise<DeleteToolsetMutationData>;
};
//# sourceMappingURL=deleteToolset.d.ts.map