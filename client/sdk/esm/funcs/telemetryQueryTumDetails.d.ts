import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TumDetailsResult } from "../models/components/tumdetailsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { QueryTumDetailsRequest, QueryTumDetailsSecurity } from "../models/operations/querytumdetails.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * queryTumDetails telemetry
 *
 * @remarks
 * Org-scoped daily usage details for the billing page's metrics table, computed in one pass: token type sums, session/tool-call/active-user counts, attribution slices (MCP tools, skills, unattributed users), and message-level stats (tokens in messages with active risk findings, tokens in tool-call messages).
 */
export declare function telemetryQueryTumDetails(client: GramCore, request: QueryTumDetailsRequest, security?: QueryTumDetailsSecurity | undefined, options?: RequestOptions): APIPromise<Result<TumDetailsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=telemetryQueryTumDetails.d.ts.map