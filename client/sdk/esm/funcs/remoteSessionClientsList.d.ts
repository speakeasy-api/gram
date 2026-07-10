import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListRemoteSessionClientsRequest, ListRemoteSessionClientsResponse, ListRemoteSessionClientsSecurity } from "../models/operations/listremotesessionclients.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listRemoteSessionClients remoteSessionClients
 *
 * @remarks
 * List remote_session_clients in the caller's project.
 */
export declare function remoteSessionClientsList(client: GramCore, request?: ListRemoteSessionClientsRequest | undefined, security?: ListRemoteSessionClientsSecurity | undefined, options?: RequestOptions): APIPromise<PageIterator<Result<ListRemoteSessionClientsResponse, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>, {
    cursor: string;
}>>;
//# sourceMappingURL=remoteSessionClientsList.d.ts.map