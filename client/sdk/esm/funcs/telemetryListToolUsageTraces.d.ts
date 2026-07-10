import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolUsageTracesResult } from "../models/components/listtoolusagetracesresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListToolUsageTracesRequest, ListToolUsageTracesSecurity } from "../models/operations/listtoolusagetraces.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listToolUsageTraces telemetry
 *
 * @remarks
 * List target-aware MCP and tool usage traces
 */
export declare function telemetryListToolUsageTraces(client: GramCore, request: ListToolUsageTracesRequest, security?: ListToolUsageTracesSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListToolUsageTracesResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=telemetryListToolUsageTraces.d.ts.map