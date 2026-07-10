import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteCustomDetectionRuleRequest, DeleteCustomDetectionRuleSecurity } from "../models/operations/deletecustomdetectionrule.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteCustomDetectionRule risk
 *
 * @remarks
 * Delete a custom detection rule.
 */
export declare function riskCustomRulesDelete(client: GramCore, request: DeleteCustomDetectionRuleRequest, security?: DeleteCustomDetectionRuleSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskCustomRulesDelete.d.ts.map