import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UploadOpenAPIv3Result } from "../models/components/uploadopenapiv3result.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { FetchOpenAPIv3FromURLRequest, FetchOpenAPIv3FromURLSecurity } from "../models/operations/fetchopenapiv3fromurl.js";
import { MutationHookOptions } from "./_types.js";
export type FetchOpenAPIv3FromURLMutationVariables = {
    request: FetchOpenAPIv3FromURLRequest;
    security?: FetchOpenAPIv3FromURLSecurity | undefined;
    options?: RequestOptions;
};
export type FetchOpenAPIv3FromURLMutationData = UploadOpenAPIv3Result;
export type FetchOpenAPIv3FromURLMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * fetchOpenAPIv3FromURL assets
 *
 * @remarks
 * Fetch an OpenAPI v3 document from a URL and upload it to Gram.
 */
export declare function useFetchOpenAPIv3FromURLMutation(options?: MutationHookOptions<FetchOpenAPIv3FromURLMutationData, FetchOpenAPIv3FromURLMutationError, FetchOpenAPIv3FromURLMutationVariables>): UseMutationResult<FetchOpenAPIv3FromURLMutationData, FetchOpenAPIv3FromURLMutationError, FetchOpenAPIv3FromURLMutationVariables>;
export declare function mutationKeyFetchOpenAPIv3FromURL(): MutationKey;
export declare function buildFetchOpenAPIv3FromURLMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: FetchOpenAPIv3FromURLMutationVariables) => Promise<FetchOpenAPIv3FromURLMutationData>;
};
//# sourceMappingURL=fetchOpenAPIv3FromURL.d.ts.map