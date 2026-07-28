import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PeriodUsage } from "../models/components/periodusage.js";
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
  GetPeriodUsageRequest,
  GetPeriodUsageSecurity,
} from "../models/operations/getperiodusage.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getPeriodUsage usage
 *
 * @remarks
 * Get the usage for an organization for a given period
 */
export declare function usageGetPeriodUsage(
  client: GramCore,
  request?: GetPeriodUsageRequest | undefined,
  security?: GetPeriodUsageSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    PeriodUsage,
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
//# sourceMappingURL=usageGetPeriodUsage.d.ts.map
