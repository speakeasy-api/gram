import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UploadOpenAPIv3Result } from "../models/components/uploadopenapiv3result.js";
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
  UploadOpenAPIv3AssetRequest,
  UploadOpenAPIv3AssetSecurity,
} from "../models/operations/uploadopenapiv3asset.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * uploadOpenAPIv3 assets
 *
 * @remarks
 * Upload an OpenAPI v3 document to Gram.
 */
export declare function assetsUploadOpenAPIv3(
  client: GramCore,
  request: UploadOpenAPIv3AssetRequest,
  security?: UploadOpenAPIv3AssetSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    UploadOpenAPIv3Result,
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
//# sourceMappingURL=assetsUploadOpenAPIv3.d.ts.map
