import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateCheckoutRequest, CreateCheckoutSecurity } from "../models/operations/createcheckout.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createCheckout usage
 *
 * @remarks
 * Create a checkout link for upgrading to the business plan
 */
export declare function usageCreateCheckout(client: GramCore, request?: CreateCheckoutRequest | undefined, security?: CreateCheckoutSecurity | undefined, options?: RequestOptions): APIPromise<Result<string, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=usageCreateCheckout.d.ts.map