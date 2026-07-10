import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetObservabilityOverviewResult } from "../models/components/getobservabilityoverviewresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetObservabilityOverviewRequest, GetObservabilityOverviewSecurity } from "../models/operations/getobservabilityoverview.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getObservabilityOverview telemetry
 *
 * @remarks
 * Get observability overview metrics including time series, tool breakdowns, and summary stats
 */
export declare function telemetryGetObservabilityOverview(client: GramCore, request: GetObservabilityOverviewRequest, security?: GetObservabilityOverviewSecurity | undefined, options?: RequestOptions): APIPromise<Result<GetObservabilityOverviewResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=telemetryGetObservabilityOverview.d.ts.map