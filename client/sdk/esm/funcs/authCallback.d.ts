import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { AuthCallbackRequest, AuthCallbackResponse } from "../models/operations/authcallback.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * callback auth
 *
 * @remarks
 * Handles the authentication callback.
 */
export declare function authCallback(client: GramCore, request: AuthCallbackRequest, options?: RequestOptions): APIPromise<Result<AuthCallbackResponse | undefined, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=authCallback.d.ts.map