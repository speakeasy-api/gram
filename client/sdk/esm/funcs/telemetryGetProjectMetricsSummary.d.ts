import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetMetricsSummaryResult } from "../models/components/getmetricssummaryresult.js";
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
  GetProjectMetricsSummaryRequest,
  GetProjectMetricsSummarySecurity,
} from "../models/operations/getprojectmetricssummary.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getProjectMetricsSummary telemetry
 *
 * @remarks
 * Get aggregated metrics summary for an entire project
 */
export declare function telemetryGetProjectMetricsSummary(
  client: GramCore,
  request: GetProjectMetricsSummaryRequest,
  security?: GetProjectMetricsSummarySecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    GetMetricsSummaryResult,
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
//# sourceMappingURL=telemetryGetProjectMetricsSummary.d.ts.map
