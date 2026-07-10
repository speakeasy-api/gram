import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetRemoteSessionClientRequest, GetRemoteSessionClientSecurity } from "../models/operations/getremotesessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getRemoteSessionClient remoteSessionClients
 *
 * @remarks
 * Get a remote_session_client by id.
 */
export declare function remoteSessionClientsGet(client: GramCore, request: GetRemoteSessionClientRequest, security?: GetRemoteSessionClientSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteSessionClient, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteSessionClientsGet.d.ts.map