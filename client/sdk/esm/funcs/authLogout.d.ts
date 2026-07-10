import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { LogoutRequest, LogoutResponse, LogoutSecurity } from "../models/operations/logout.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * logout auth
 *
 * @remarks
 * Logs out the current user by clearing their session.
 */
export declare function authLogout(client: GramCore, request?: LogoutRequest | undefined, security?: LogoutSecurity | undefined, options?: RequestOptions): APIPromise<Result<LogoutResponse | undefined, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=authLogout.d.ts.map