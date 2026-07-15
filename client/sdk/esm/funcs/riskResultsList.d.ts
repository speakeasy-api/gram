import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskResultsResult } from "../models/components/listriskresultsresult.js";
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
  ListRiskResultsRequest,
  ListRiskResultsSecurity,
} from "../models/operations/listriskresults.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listRiskResults risk
 *
 * @remarks
 * List risk analysis results for the current project.
 */
export declare function riskResultsList(
  client: GramCore,
  request?: ListRiskResultsRequest | undefined,
  security?: ListRiskResultsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListRiskResultsResult,
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
//# sourceMappingURL=riskResultsList.d.ts.map
