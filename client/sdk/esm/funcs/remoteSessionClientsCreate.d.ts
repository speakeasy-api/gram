import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateRemoteSessionClientRequest, CreateRemoteSessionClientSecurity } from "../models/operations/createremotesessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createRemoteSessionClient remoteSessionClients
 *
 * @remarks
 * Register a remote_session_client by supplying a client_id and optional client_secret obtained out-of-band from the upstream issuer.
 */
export declare function remoteSessionClientsCreate(client: GramCore, request: CreateRemoteSessionClientRequest, security?: CreateRemoteSessionClientSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteSessionClient, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteSessionClientsCreate.d.ts.map