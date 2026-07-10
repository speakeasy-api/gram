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
export type DeleteSlackAppMutationVariables = {
    request: operations.DeleteSlackAppRequest;
    security?: operations.DeleteSlackAppSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteSlackAppMutationData = void;
export type DeleteSlackAppMutationError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteSlackApp slack
 *
 * @remarks
 * Soft-delete a Slack app.
 */
export declare function useDeleteSlackAppMutation(options?: MutationHookOptions<DeleteSlackAppMutationData, DeleteSlackAppMutationError, DeleteSlackAppMutationVariables>): UseMutationResult<DeleteSlackAppMutationData, DeleteSlackAppMutationError, DeleteSlackAppMutationVariables>;
export declare function mutationKeyDeleteSlackApp(): MutationKey;
export declare function buildDeleteSlackAppMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteSlackAppMutationVariables) => Promise<DeleteSlackAppMutationData>;
};
//# sourceMappingURL=deleteSlackApp.d.ts.map