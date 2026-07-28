import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskOverviewResult } from "../models/components/riskoverviewresult.js";
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
  GetRiskOverviewRequest,
  GetRiskOverviewSecurity,
} from "../models/operations/getriskoverview.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getRiskOverview risk
 *
 * @remarks
 * Get risk overview metrics and trend data for the current project.
 */
export declare function riskOverviewGet(
  client: GramCore,
  request?: GetRiskOverviewRequest | undefined,
  security?: GetRiskOverviewSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RiskOverviewResult,
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
//# sourceMappingURL=riskOverviewGet.d.ts.map
