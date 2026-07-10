import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetToolUsageSummaryResult } from "../models/components/gettoolusagesummaryresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetToolUsageSummaryRequest, GetToolUsageSummarySecurity } from "../models/operations/gettoolusagesummary.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getToolUsageSummary telemetry
 *
 * @remarks
 * Get target-aware MCP and tool usage metrics
 */
export declare function telemetryGetToolUsageSummary(client: GramCore, request: GetToolUsageSummaryRequest, security?: GetToolUsageSummarySecurity | undefined, options?: RequestOptions): APIPromise<Result<GetToolUsageSummaryResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=telemetryGetToolUsageSummary.d.ts.map