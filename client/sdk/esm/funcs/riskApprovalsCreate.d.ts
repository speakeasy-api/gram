import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * approveShadowMCP risk
 *
 * @remarks
 * Approve a shadow-MCP server so the named policy stops blocking calls to it. `match` is the same opaque server identifier surfaced in `RiskResult.match` — typically a server URL, stdio command, or `mcp__<server>__` prefix.
 */
export declare function riskApprovalsCreate(client: GramCore, request: operations.ApproveShadowMCPRequest, security?: operations.ApproveShadowMCPSecurity | undefined, options?: RequestOptions): APIPromise<Result<components.ShadowMCPApproval, errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskApprovalsCreate.d.ts.map