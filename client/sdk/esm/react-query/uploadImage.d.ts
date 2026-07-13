import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UploadImageResult } from "../models/components/uploadimageresult.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  UploadImageRequest,
  UploadImageSecurity,
} from "../models/operations/uploadimage.js";
import { MutationHookOptions } from "./_types.js";
export type UploadImageMutationVariables = {
  request: UploadImageRequest;
  security?: UploadImageSecurity | undefined;
  options?: RequestOptions;
};
export type UploadImageMutationData = UploadImageResult;
export type UploadImageMutationError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * uploadImage assets
 *
 * @remarks
 * Upload an image to Gram.
 */
export declare function useUploadImageMutation(
  options?: MutationHookOptions<
    UploadImageMutationData,
    UploadImageMutationError,
    UploadImageMutationVariables
  >,
): UseMutationResult<
  UploadImageMutationData,
  UploadImageMutationError,
  UploadImageMutationVariables
>;
export declare function mutationKeyUploadImage(): MutationKey;
export declare function buildUploadImageMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UploadImageMutationVariables,
  ) => Promise<UploadImageMutationData>;
};
//# sourceMappingURL=uploadImage.d.ts.map
