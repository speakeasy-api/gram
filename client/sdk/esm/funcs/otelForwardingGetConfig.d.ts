import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OtelForwardingConfig } from "../models/components/otelforwardingconfig.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetOtelForwardingConfigRequest, GetOtelForwardingConfigSecurity } from "../models/operations/getotelforwardingconfig.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getConfig otelForwarding
 *
 * @remarks
 * Get the org-wide OTEL forwarding config. Returns an empty config (enabled=false, no URL) when none is set.
 */
export declare function otelForwardingGetConfig(client: GramCore, request?: GetOtelForwardingConfigRequest | undefined, security?: GetOtelForwardingConfigSecurity | undefined, options?: RequestOptions): APIPromise<Result<OtelForwardingConfig, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=otelForwardingGetConfig.d.ts.map