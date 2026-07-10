import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OtelForwardingConfig } from "../models/components/otelforwardingconfig.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpsertOtelForwardingConfigRequest, UpsertOtelForwardingConfigSecurity } from "../models/operations/upsertotelforwardingconfig.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * upsertConfig otelForwarding
 *
 * @remarks
 * Create or update the org-wide OTEL forwarding config. Replaces the full header set on each call.
 */
export declare function otelForwardingUpsertConfig(client: GramCore, request: UpsertOtelForwardingConfigRequest, security?: UpsertOtelForwardingConfigSecurity | undefined, options?: RequestOptions): APIPromise<Result<OtelForwardingConfig, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=otelForwardingUpsertConfig.d.ts.map