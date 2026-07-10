import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateDestination otelForwarding
 *
 * @remarks
 * Replace every mutable field on a forwarding destination. Headers are fully replaced — pass the full set on each call.
 */
export declare function otelForwardingUpdateDestination(client: GramCore, request: operations.UpdateOtelForwardingDestinationRequest, security?: operations.UpdateOtelForwardingDestinationSecurity | undefined, options?: RequestOptions): APIPromise<Result<components.OtelForwardingDestination, errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=otelForwardingUpdateDestination.d.ts.map