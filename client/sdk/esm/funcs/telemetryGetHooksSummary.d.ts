import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetHooksSummaryResult } from "../models/components/gethookssummaryresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetHooksSummaryRequest, GetHooksSummarySecurity } from "../models/operations/gethookssummary.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getHooksSummary telemetry
 *
 * @remarks
 * Get aggregated hooks metrics grouped by server
 */
export declare function telemetryGetHooksSummary(client: GramCore, request: GetHooksSummaryRequest, security?: GetHooksSummarySecurity | undefined, options?: RequestOptions): APIPromise<Result<GetHooksSummaryResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=telemetryGetHooksSummary.d.ts.map