import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * refresh tokenExchange
 *
 * @remarks
 * Rotate a refresh token for a fresh user-scoped access JWT and a newly rotated (single-use) refresh token. The opaque refresh token in the body is self-authenticating; no API key is required.
 */
export declare function tokenExchangeRefresh(
  client: GramCore,
  request: components.RefreshRequestBody,
  options?: RequestOptions,
): APIPromise<
  Result<
    components.TokenResult,
    | errors.ServiceError
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
//# sourceMappingURL=tokenExchangeRefresh.d.ts.map
