import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SearchLogsResult } from "../models/components/searchlogsresult.js";
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
  SearchLogsRequest,
  SearchLogsSecurity,
} from "../models/operations/searchlogs.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * searchLogs telemetry
 *
 * @remarks
 * Search and list telemetry logs that match a search filter
 */
export declare function telemetrySearchLogs(
  client: GramCore,
  request: SearchLogsRequest,
  security?: SearchLogsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    SearchLogsResult,
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
//# sourceMappingURL=telemetrySearchLogs.d.ts.map
