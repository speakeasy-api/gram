import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreateShadowMCPAccessRuleResult } from "../models/components/createshadowmcpaccessruleresult.js";
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
  CreateShadowMCPAccessRuleRequest,
  CreateShadowMCPAccessRuleSecurity,
} from "../models/operations/createshadowmcpaccessrule.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createShadowMCPAccessRule access
 *
 * @remarks
 * Create a managed Shadow MCP access rule.
 */
export declare function accessCreateShadowMCPAccessRule(
  client: GramCore,
  request: CreateShadowMCPAccessRuleRequest,
  security?: CreateShadowMCPAccessRuleSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    CreateShadowMCPAccessRuleResult,
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
//# sourceMappingURL=accessCreateShadowMCPAccessRule.d.ts.map
