import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ValidateKeyResult } from "../models/components/validatekeyresult.js";
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
  ValidateAPIKeyRequest,
  ValidateAPIKeySecurity,
} from "../models/operations/validateapikey.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * verifyKey keys
 *
 * @remarks
 * Verify an api key
 */
export declare function keysValidate(
  client: GramCore,
  request?: ValidateAPIKeyRequest | undefined,
  security?: ValidateAPIKeySecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ValidateKeyResult,
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
//# sourceMappingURL=keysValidate.d.ts.map
