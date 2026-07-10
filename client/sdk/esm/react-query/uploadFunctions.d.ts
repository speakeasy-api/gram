import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UploadFunctionsResult } from "../models/components/uploadfunctionsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UploadFunctionsRequest, UploadFunctionsSecurity } from "../models/operations/uploadfunctions.js";
import { MutationHookOptions } from "./_types.js";
export type UploadFunctionsMutationVariables = {
    request: UploadFunctionsRequest;
    security?: UploadFunctionsSecurity | undefined;
    options?: RequestOptions;
};
export type UploadFunctionsMutationData = UploadFunctionsResult;
export type UploadFunctionsMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * uploadFunctions assets
 *
 * @remarks
 * Upload functions to Gram.
 */
export declare function useUploadFunctionsMutation(options?: MutationHookOptions<UploadFunctionsMutationData, UploadFunctionsMutationError, UploadFunctionsMutationVariables>): UseMutationResult<UploadFunctionsMutationData, UploadFunctionsMutationError, UploadFunctionsMutationVariables>;
export declare function mutationKeyUploadFunctions(): MutationKey;
export declare function buildUploadFunctionsMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UploadFunctionsMutationVariables) => Promise<UploadFunctionsMutationData>;
};
//# sourceMappingURL=uploadFunctions.d.ts.map