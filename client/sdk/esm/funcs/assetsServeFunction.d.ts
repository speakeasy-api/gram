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
  ServeFunctionRequest,
  ServeFunctionResponse,
  ServeFunctionSecurity,
} from "../models/operations/servefunction.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * serveFunction assets
 *
 * @remarks
 * Serve a Gram Functions asset from Gram.
 */
export declare function assetsServeFunction(
  client: GramCore,
  request: ServeFunctionRequest,
  security?: ServeFunctionSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ServeFunctionResponse,
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
//# sourceMappingURL=assetsServeFunction.d.ts.map
