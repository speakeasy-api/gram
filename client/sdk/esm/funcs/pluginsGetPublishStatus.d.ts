import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PublishStatusResult } from "../models/components/publishstatusresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetPublishStatusRequest, GetPublishStatusSecurity } from "../models/operations/getpublishstatus.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getPublishStatus plugins
 *
 * @remarks
 * Check whether GitHub publishing is configured and connected for this project.
 */
export declare function pluginsGetPublishStatus(client: GramCore, request?: GetPublishStatusRequest | undefined, security?: GetPublishStatusSecurity | undefined, options?: RequestOptions): APIPromise<Result<PublishStatusResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=pluginsGetPublishStatus.d.ts.map