import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateCustomerSessionRequest, CreateCustomerSessionSecurity } from "../models/operations/createcustomersession.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createCustomerSession usage
 *
 * @remarks
 * Create a customer session for the user
 */
export declare function usageCreateCustomerSession(client: GramCore, request?: CreateCustomerSessionRequest | undefined, security?: CreateCustomerSessionSecurity | undefined, options?: RequestOptions): APIPromise<Result<string, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=usageCreateCustomerSession.d.ts.map