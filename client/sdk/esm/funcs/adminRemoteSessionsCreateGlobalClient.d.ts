import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateGlobalRemoteSessionClientRequest, CreateGlobalRemoteSessionClientSecurity } from "../models/operations/createglobalremotesessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createGlobalClient adminRemoteSessions
 *
 * @remarks
 * Register a global remote_session_client under an existing global remote_session_issuer. Caller supplies client_id and optional client_secret obtained out-of-band from the upstream issuer. Requires platform admin.
 */
export declare function adminRemoteSessionsCreateGlobalClient(client: GramCore, request: CreateGlobalRemoteSessionClientRequest, security?: CreateGlobalRemoteSessionClientSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteSessionClient, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=adminRemoteSessionsCreateGlobalClient.d.ts.map