import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { DeleteGlobalToolVariationResult } from "../models/components/deleteglobaltoolvariationresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteGlobalVariationRequest, DeleteGlobalVariationSecurity } from "../models/operations/deleteglobalvariation.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteGlobalVariationMutationVariables = {
    request: DeleteGlobalVariationRequest;
    security?: DeleteGlobalVariationSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteGlobalVariationMutationData = DeleteGlobalToolVariationResult;
export type DeleteGlobalVariationMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteGlobal variations
 *
 * @remarks
 * Create or update a globally defined tool variation.
 */
export declare function useDeleteGlobalVariationMutation(options?: MutationHookOptions<DeleteGlobalVariationMutationData, DeleteGlobalVariationMutationError, DeleteGlobalVariationMutationVariables>): UseMutationResult<DeleteGlobalVariationMutationData, DeleteGlobalVariationMutationError, DeleteGlobalVariationMutationVariables>;
export declare function mutationKeyDeleteGlobalVariation(): MutationKey;
export declare function buildDeleteGlobalVariationMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteGlobalVariationMutationVariables) => Promise<DeleteGlobalVariationMutationData>;
};
//# sourceMappingURL=deleteGlobalVariation.d.ts.map