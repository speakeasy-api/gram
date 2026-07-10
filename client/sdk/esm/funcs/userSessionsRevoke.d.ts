import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RevokeUserSessionRequest, RevokeUserSessionSecurity } from "../models/operations/revokeusersession.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * revokeUserSession userSessions
 *
 * @remarks
 * Push the session's jti into the revocation cache and soft-delete the row.
 */
export declare function userSessionsRevoke(client: GramCore, request: RevokeUserSessionRequest, security?: RevokeUserSessionSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=userSessionsRevoke.d.ts.map