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
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * uploadImage assets
 *
 * @remarks
 * Upload an image to Gram.
 */
export declare function assetsUploadImage(
  client: GramCore,
  request: UploadImageRequest,
  security?: UploadImageSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    UploadImageResult,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=assetsUploadImage.d.ts.map
