import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteOtelForwardingDestinationMutationVariables = {
    request: operations.DeleteOtelForwardingDestinationRequest;
    security?: operations.DeleteOtelForwardingDestinationSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteOtelForwardingDestinationMutationData = void;
export type DeleteOtelForwardingDestinationMutationError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteDestination otelForwarding
 *
 * @remarks
 * Delete a forwarding destination.
 */
export declare function useDeleteOtelForwardingDestinationMutation(options?: MutationHookOptions<DeleteOtelForwardingDestinationMutationData, DeleteOtelForwardingDestinationMutationError, DeleteOtelForwardingDestinationMutationVariables>): UseMutationResult<DeleteOtelForwardingDestinationMutationData, DeleteOtelForwardingDestinationMutationError, DeleteOtelForwardingDestinationMutationVariables>;
export declare function mutationKeyDeleteOtelForwardingDestination(): MutationKey;
export declare function buildDeleteOtelForwardingDestinationMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteOtelForwardingDestinationMutationVariables) => Promise<DeleteOtelForwardingDestinationMutationData>;
};
//# sourceMappingURL=deleteOtelForwardingDestination.d.ts.map