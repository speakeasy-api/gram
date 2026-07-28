import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listDestinations otelForwarding
 *
 * @remarks
 * List every OTEL forwarding destination configured for the organization.
 */
export declare function otelForwardingListDestinations(
  client: GramCore,
  request?: operations.ListOtelForwardingDestinationsRequest | undefined,
  security?: operations.ListOtelForwardingDestinationsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    components.OtelForwardingDestinationList,
    | errors.ServiceError
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
//# sourceMappingURL=otelForwardingListDestinations.d.ts.map
