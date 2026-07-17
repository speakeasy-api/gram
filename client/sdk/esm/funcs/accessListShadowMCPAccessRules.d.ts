import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListShadowMCPAccessRulesResult } from "../models/components/listshadowmcpaccessrulesresult.js";
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
  ListShadowMCPAccessRulesRequest,
  ListShadowMCPAccessRulesSecurity,
} from "../models/operations/listshadowmcpaccessrules.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listShadowMCPAccessRules access
 *
 * @remarks
 * List managed Shadow MCP allow and deny rules.
 */
export declare function accessListShadowMCPAccessRules(
  client: GramCore,
  request?: ListShadowMCPAccessRulesRequest | undefined,
  security?: ListShadowMCPAccessRulesSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListShadowMCPAccessRulesResult,
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
//# sourceMappingURL=accessListShadowMCPAccessRules.d.ts.map
