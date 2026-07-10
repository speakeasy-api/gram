import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  ServeImageRequest,
  ServeImageResponse,
} from "../models/operations/serveimage.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * serveImage assets
 *
 * @remarks
 * Serve an image from Gram.
 */
export declare function assetsServeImage(
  client: GramCore,
  request: ServeImageRequest,
  options?: RequestOptions,
): APIPromise<
  Result<
    ServeImageResponse,
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
//# sourceMappingURL=assetsServeImage.d.ts.map
