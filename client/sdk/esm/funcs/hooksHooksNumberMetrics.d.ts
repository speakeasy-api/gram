import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  HooksNumberMetricsRequest,
  HooksNumberMetricsSecurity,
} from "../models/operations/hooksnumbermetrics.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * metrics hooks
 *
 * @remarks
 * Endpoint to receive OTEL metrics data from Claude Code. Requires API key authentication.
 */
export declare function hooksHooksNumberMetrics(
  client: GramCore,
  request: HooksNumberMetricsRequest,
  security?: HooksNumberMetricsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    void,
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
//# sourceMappingURL=hooksHooksNumberMetrics.d.ts.map
