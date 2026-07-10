import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UsageTiers } from "../models/components/usagetiers.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getUsageTiers usage
 *
 * @remarks
 * Get the usage tiers
 */
export declare function usageGetUsageTiers(client: GramCore, options?: RequestOptions): APIPromise<Result<UsageTiers, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=usageGetUsageTiers.d.ts.map