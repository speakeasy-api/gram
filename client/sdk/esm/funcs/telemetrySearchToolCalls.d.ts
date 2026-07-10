import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SearchToolCallsResult } from "../models/components/searchtoolcallsresult.js";
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
  SearchToolCallsRequest,
  SearchToolCallsSecurity,
} from "../models/operations/searchtoolcalls.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * searchToolCalls telemetry
 *
 * @remarks
 * Search and list tool calls that match a search filter
 */
export declare function telemetrySearchToolCalls(
  client: GramCore,
  request: SearchToolCallsRequest,
  security?: SearchToolCallsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    SearchToolCallsResult,
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
//# sourceMappingURL=telemetrySearchToolCalls.d.ts.map
