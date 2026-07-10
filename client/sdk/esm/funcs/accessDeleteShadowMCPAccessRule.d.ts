import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteShadowMCPAccessRuleRequest, DeleteShadowMCPAccessRuleSecurity } from "../models/operations/deleteshadowmcpaccessrule.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteShadowMCPAccessRule access
 *
 * @remarks
 * Delete a managed Shadow MCP access rule.
 */
export declare function accessDeleteShadowMCPAccessRule(client: GramCore, request: DeleteShadowMCPAccessRuleRequest, security?: DeleteShadowMCPAccessRuleSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=accessDeleteShadowMCPAccessRule.d.ts.map