import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteGlobalRemoteSessionClientRequest, DeleteGlobalRemoteSessionClientSecurity } from "../models/operations/deleteglobalremotesessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteGlobalClient adminRemoteSessions
 *
 * @remarks
 * Soft-delete a global remote_session_client. Cascades to the remote_sessions minted against it. Requires platform admin.
 */
export declare function adminRemoteSessionsDeleteGlobalClient(client: GramCore, request: DeleteGlobalRemoteSessionClientRequest, security?: DeleteGlobalRemoteSessionClientSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=adminRemoteSessionsDeleteGlobalClient.d.ts.map