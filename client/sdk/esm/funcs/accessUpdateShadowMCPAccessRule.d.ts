import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ShadowMCPAccessRule } from "../models/components/shadowmcpaccessrule.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateShadowMCPAccessRuleRequest, UpdateShadowMCPAccessRuleSecurity } from "../models/operations/updateshadowmcpaccessrule.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateShadowMCPAccessRule access
 *
 * @remarks
 * Update a managed Shadow MCP access rule.
 */
export declare function accessUpdateShadowMCPAccessRule(client: GramCore, request: UpdateShadowMCPAccessRuleRequest, security?: UpdateShadowMCPAccessRuleSecurity | undefined, options?: RequestOptions): APIPromise<Result<ShadowMCPAccessRule, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=accessUpdateShadowMCPAccessRule.d.ts.map