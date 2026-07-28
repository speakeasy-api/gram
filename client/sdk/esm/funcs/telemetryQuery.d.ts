import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { QueryResult } from "../models/components/queryresult.js";
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
import { QueryRequest, QuerySecurity } from "../models/operations/query.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * query telemetry
 *
 * @remarks
 * Generic, org-scoped analytics query over pre-aggregated usage metrics. Returns both a grouped table and a per-group hourly timeseries for the same slice of data, supporting arbitrary allowlisted group-by dimensions and filters (e.g. group by department_name, then drill in by filtering department_name and grouping by role).
 */
export declare function telemetryQuery(
  client: GramCore,
  request: QueryRequest,
  security?: QuerySecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    QueryResult,
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
//# sourceMappingURL=telemetryQuery.d.ts.map
