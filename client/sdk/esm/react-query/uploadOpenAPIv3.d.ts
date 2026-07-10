import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UploadOpenAPIv3Result } from "../models/components/uploadopenapiv3result.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UploadOpenAPIv3AssetRequest, UploadOpenAPIv3AssetSecurity } from "../models/operations/uploadopenapiv3asset.js";
import { MutationHookOptions } from "./_types.js";
export type UploadOpenAPIv3MutationVariables = {
    request: UploadOpenAPIv3AssetRequest;
    security?: UploadOpenAPIv3AssetSecurity | undefined;
    options?: RequestOptions;
};
export type UploadOpenAPIv3MutationData = UploadOpenAPIv3Result;
export type UploadOpenAPIv3MutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * uploadOpenAPIv3 assets
 *
 * @remarks
 * Upload an OpenAPI v3 document to Gram.
 */
export declare function useUploadOpenAPIv3Mutation(options?: MutationHookOptions<UploadOpenAPIv3MutationData, UploadOpenAPIv3MutationError, UploadOpenAPIv3MutationVariables>): UseMutationResult<UploadOpenAPIv3MutationData, UploadOpenAPIv3MutationError, UploadOpenAPIv3MutationVariables>;
export declare function mutationKeyUploadOpenAPIv3(): MutationKey;
export declare function buildUploadOpenAPIv3Mutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UploadOpenAPIv3MutationVariables) => Promise<UploadOpenAPIv3MutationData>;
};
//# sourceMappingURL=uploadOpenAPIv3.d.ts.map