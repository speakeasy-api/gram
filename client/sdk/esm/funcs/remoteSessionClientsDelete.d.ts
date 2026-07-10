import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteRemoteSessionClientRequest, DeleteRemoteSessionClientSecurity } from "../models/operations/deleteremotesessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteRemoteSessionClient remoteSessionClients
 *
 * @remarks
 * Soft-delete a remote_session_client. Cascades to remote_sessions rows pointing at this client; affected principals are forced to re-authenticate.
 */
export declare function remoteSessionClientsDelete(client: GramCore, request: DeleteRemoteSessionClientRequest, security?: DeleteRemoteSessionClientSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteSessionClientsDelete.d.ts.map