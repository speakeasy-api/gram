import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListSessionsResult } from "../models/components/listsessionsresult.js";
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
  ListSessionsRequest,
  ListSessionsSecurity,
} from "../models/operations/listsessions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listSessions telemetry
 *
 * @remarks
 * Org-scoped list of individual chat sessions for a slice of usage, filtered by the same allowlisted dimensions as telemetry.query. Returns per-session cost, token, and tool metrics with cursor pagination.
 */
export declare function telemetryListSessions(
  client: GramCore,
  request: ListSessionsRequest,
  security?: ListSessionsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListSessionsResult,
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
//# sourceMappingURL=telemetryListSessions.d.ts.map
