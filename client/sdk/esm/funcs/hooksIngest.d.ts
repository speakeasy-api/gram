import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { IngestHookResult } from "../models/components/ingesthookresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { IngestHookEventRequest, IngestHookEventSecurity } from "../models/operations/ingesthookevent.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * ingest hooks
 *
 * @remarks
 * Feature-first unified endpoint for hook events from supported coding assistants.
 */
export declare function hooksIngest(client: GramCore, request: IngestHookEventRequest, security?: IngestHookEventSecurity | undefined, options?: RequestOptions): APIPromise<Result<IngestHookResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=hooksIngest.d.ts.map