import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskUserBreakdownResult } from "../models/components/riskuserbreakdownresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetRiskUserBreakdownRequest, GetRiskUserBreakdownSecurity } from "../models/operations/getriskuserbreakdown.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getRiskUserBreakdown risk
 *
 * @remarks
 * Per-user breakdowns of findings by category and by rule_id within a time window. Powers the user drill-down on /risk-overview.
 */
export declare function riskOverviewUserBreakdown(client: GramCore, request: GetRiskUserBreakdownRequest, security?: GetRiskUserBreakdownSecurity | undefined, options?: RequestOptions): APIPromise<Result<RiskUserBreakdownResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskOverviewUserBreakdown.d.ts.map