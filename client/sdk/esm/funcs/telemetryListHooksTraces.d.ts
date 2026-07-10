import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListHooksTracesResult } from "../models/components/listhookstracesresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListHooksTracesRequest, ListHooksTracesSecurity } from "../models/operations/listhookstraces.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listHooksTraces telemetry
 *
 * @remarks
 * List hook traces aggregated by trace_id with user information
 */
export declare function telemetryListHooksTraces(client: GramCore, request: ListHooksTracesRequest, security?: ListHooksTracesSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListHooksTracesResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=telemetryListHooksTraces.d.ts.map