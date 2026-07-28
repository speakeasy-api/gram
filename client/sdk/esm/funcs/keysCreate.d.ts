import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Key } from "../models/components/key.js";
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
  CreateAPIKeyRequest,
  CreateAPIKeySecurity,
} from "../models/operations/createapikey.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createKey keys
 *
 * @remarks
 * Create a new api key
 */
export declare function keysCreate(
  client: GramCore,
  request: CreateAPIKeyRequest,
  security?: CreateAPIKeySecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    Key,
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
//# sourceMappingURL=keysCreate.d.ts.map
