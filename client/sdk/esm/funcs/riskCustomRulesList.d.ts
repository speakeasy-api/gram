import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListCustomDetectionRulesResult } from "../models/components/listcustomdetectionrulesresult.js";
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
  ListCustomDetectionRulesRequest,
  ListCustomDetectionRulesSecurity,
} from "../models/operations/listcustomdetectionrules.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listCustomDetectionRules risk
 *
 * @remarks
 * List custom detection rules for the current project.
 */
export declare function riskCustomRulesList(
  client: GramCore,
  request?: ListCustomDetectionRulesRequest | undefined,
  security?: ListCustomDetectionRulesSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListCustomDetectionRulesResult,
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
//# sourceMappingURL=riskCustomRulesList.d.ts.map
