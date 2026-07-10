import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RevokeUserSessionClientRequest, RevokeUserSessionClientSecurity } from "../models/operations/revokeusersessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * revokeUserSessionClient userSessionClients
 *
 * @remarks
 * Soft-delete a user_session_client. Future tokens minted for this client_id are rejected; existing live user_sessions keep working until they hit expires_at.
 */
export declare function userSessionClientsRevoke(client: GramCore, request: RevokeUserSessionClientRequest, security?: RevokeUserSessionClientSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=userSessionClientsRevoke.d.ts.map