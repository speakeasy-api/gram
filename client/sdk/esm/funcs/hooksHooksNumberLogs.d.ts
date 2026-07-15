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
  HooksNumberLogsRequest,
  HooksNumberLogsSecurity,
} from "../models/operations/hooksnumberlogs.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * logs hooks
 *
 * @remarks
 * Endpoint to receive OTEL logs data from Claude Code. Requires API key authentication.
 */
export declare function hooksHooksNumberLogs(
  client: GramCore,
  request: HooksNumberLogsRequest,
  security?: HooksNumberLogsSecurity | undefined,
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
//# sourceMappingURL=hooksHooksNumberLogs.d.ts.map
