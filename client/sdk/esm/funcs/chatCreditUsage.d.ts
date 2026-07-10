import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreditUsageResponseBody } from "../models/components/creditusageresponsebody.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreditUsageRequest, CreditUsageSecurity } from "../models/operations/creditusage.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * creditUsage chat
 *
 * @remarks
 * Get the total number of chat credits and usage for the current billing period
 */
export declare function chatCreditUsage(client: GramCore, request?: CreditUsageRequest | undefined, security?: CreditUsageSecurity | undefined, options?: RequestOptions): APIPromise<Result<CreditUsageResponseBody, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=chatCreditUsage.d.ts.map