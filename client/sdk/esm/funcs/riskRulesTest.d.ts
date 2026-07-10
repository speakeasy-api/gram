import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TestDetectionRuleResult } from "../models/components/testdetectionruleresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { TestDetectionRuleRequest, TestDetectionRuleSecurity } from "../models/operations/testdetectionrule.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * testDetectionRule risk
 *
 * @remarks
 * Run a single detection rule against pasted sample text and return any matches. Reuses the same scanner code (gitleaks, Presidio, prompt-injection, custom regex) that the analyzer runs in production so the playground match shape mirrors the chat-message path.
 */
export declare function riskRulesTest(client: GramCore, request: TestDetectionRuleRequest, security?: TestDetectionRuleSecurity | undefined, options?: RequestOptions): APIPromise<Result<TestDetectionRuleResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskRulesTest.d.ts.map