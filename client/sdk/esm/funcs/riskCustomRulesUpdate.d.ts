import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskCustomDetectionRule } from "../models/components/riskcustomdetectionrule.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateCustomDetectionRuleRequest, UpdateCustomDetectionRuleSecurity } from "../models/operations/updatecustomdetectionrule.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateCustomDetectionRule risk
 *
 * @remarks
 * Update a custom detection rule.
 */
export declare function riskCustomRulesUpdate(client: GramCore, request: UpdateCustomDetectionRuleRequest, security?: UpdateCustomDetectionRuleSecurity | undefined, options?: RequestOptions): APIPromise<Result<RiskCustomDetectionRule, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskCustomRulesUpdate.d.ts.map