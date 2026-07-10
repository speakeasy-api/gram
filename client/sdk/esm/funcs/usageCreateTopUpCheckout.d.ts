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
  CreateTopUpCheckoutRequest,
  CreateTopUpCheckoutSecurity,
} from "../models/operations/createtopupcheckout.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createTopUpCheckout usage
 *
 * @remarks
 * Create a checkout link for a one-time credit top-up purchase
 */
export declare function usageCreateTopUpCheckout(
  client: GramCore,
  request?: CreateTopUpCheckoutRequest | undefined,
  security?: CreateTopUpCheckoutSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    string,
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
//# sourceMappingURL=usageCreateTopUpCheckout.d.ts.map
