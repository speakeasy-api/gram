import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetUserMetricsSummaryResult } from "../models/components/getusermetricssummaryresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetUserMetricsSummaryRequest, GetUserMetricsSummarySecurity } from "../models/operations/getusermetricssummary.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getUserMetricsSummary telemetry
 *
 * @remarks
 * Get aggregated metrics summary grouped by user
 */
export declare function telemetryGetUserMetricsSummary(client: GramCore, request: GetUserMetricsSummaryRequest, security?: GetUserMetricsSummarySecurity | undefined, options?: RequestOptions): APIPromise<Result<GetUserMetricsSummaryResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=telemetryGetUserMetricsSummary.d.ts.map