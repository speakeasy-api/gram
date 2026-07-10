import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PublishPackageResult } from "../models/components/publishpackageresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { PublishRequest, PublishSecurity } from "../models/operations/publish.js";
import { MutationHookOptions } from "./_types.js";
export type PublishPackageMutationVariables = {
    request: PublishRequest;
    security?: PublishSecurity | undefined;
    options?: RequestOptions;
};
export type PublishPackageMutationData = PublishPackageResult;
export type PublishPackageMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * publish packages
 *
 * @remarks
 * Publish a new version of a package.
 */
export declare function usePublishPackageMutation(options?: MutationHookOptions<PublishPackageMutationData, PublishPackageMutationError, PublishPackageMutationVariables>): UseMutationResult<PublishPackageMutationData, PublishPackageMutationError, PublishPackageMutationVariables>;
export declare function mutationKeyPublishPackage(): MutationKey;
export declare function buildPublishPackageMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: PublishPackageMutationVariables) => Promise<PublishPackageMutationData>;
};
//# sourceMappingURL=publishPackage.d.ts.map