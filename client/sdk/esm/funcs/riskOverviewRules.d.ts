import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskRuleBreakdownResult } from "../models/components/riskrulebreakdownresult.js";
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
  GetRiskRuleBreakdownRequest,
  GetRiskRuleBreakdownSecurity,
} from "../models/operations/getriskrulebreakdown.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getRiskRuleBreakdown risk
 *
 * @remarks
 * Get per-rule_id finding counts for a category within a time window. Powers the per-category drill-down chart on /risk-overview.
 */
export declare function riskOverviewRules(
  client: GramCore,
  request: GetRiskRuleBreakdownRequest,
  security?: GetRiskRuleBreakdownSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RiskRuleBreakdownResult,
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
//# sourceMappingURL=riskOverviewRules.d.ts.map
