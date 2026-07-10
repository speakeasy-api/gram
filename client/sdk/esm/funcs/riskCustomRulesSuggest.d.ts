import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SuggestCustomDetectionRuleResult } from "../models/components/suggestcustomdetectionruleresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { SuggestCustomDetectionRuleRequest, SuggestCustomDetectionRuleSecurity } from "../models/operations/suggestcustomdetectionrule.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * suggestCustomDetectionRule risk
 *
 * @remarks
 * Suggest a custom detection rule (rule_id, title, description, regex, severity) from a natural-language prompt. Calls the configured LLM with a JSON-schema constrained response so the dashboard can prefill the create form.
 */
export declare function riskCustomRulesSuggest(client: GramCore, request: SuggestCustomDetectionRuleRequest, security?: SuggestCustomDetectionRuleSecurity | undefined, options?: RequestOptions): APIPromise<Result<SuggestCustomDetectionRuleResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskCustomRulesSuggest.d.ts.map