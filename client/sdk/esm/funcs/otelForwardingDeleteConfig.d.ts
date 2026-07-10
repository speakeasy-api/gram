import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteOtelForwardingConfigRequest, DeleteOtelForwardingConfigSecurity } from "../models/operations/deleteotelforwardingconfig.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteConfig otelForwarding
 *
 * @remarks
 * Delete the org-wide OTEL forwarding config.
 */
export declare function otelForwardingDeleteConfig(client: GramCore, request?: DeleteOtelForwardingConfigRequest | undefined, security?: DeleteOtelForwardingConfigSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=otelForwardingDeleteConfig.d.ts.map