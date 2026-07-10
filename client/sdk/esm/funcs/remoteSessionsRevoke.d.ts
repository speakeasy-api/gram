import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RevokeRemoteSessionRequest, RevokeRemoteSessionSecurity } from "../models/operations/revokeremotesession.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * revokeRemoteSession remoteSessions
 *
 * @remarks
 * Drop a remote_session row. The next /mcp call by that principal triggers a fresh authn challenge.
 */
export declare function remoteSessionsRevoke(client: GramCore, request: RevokeRemoteSessionRequest, security?: RevokeRemoteSessionSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteSessionsRevoke.d.ts.map