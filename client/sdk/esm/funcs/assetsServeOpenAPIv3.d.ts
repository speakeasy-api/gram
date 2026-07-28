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
  ServeOpenAPIv3Request,
  ServeOpenAPIv3Response,
  ServeOpenAPIv3Security,
} from "../models/operations/serveopenapiv3.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * serveOpenAPIv3 assets
 *
 * @remarks
 * Serve an OpenAPIv3 asset from Gram.
 */
export declare function assetsServeOpenAPIv3(
  client: GramCore,
  request: ServeOpenAPIv3Request,
  security?: ServeOpenAPIv3Security | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ServeOpenAPIv3Response,
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
//# sourceMappingURL=assetsServeOpenAPIv3.d.ts.map
