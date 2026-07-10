import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListGlobalRemoteSessionClientsRequest, ListGlobalRemoteSessionClientsResponse, ListGlobalRemoteSessionClientsSecurity } from "../models/operations/listglobalremotesessionclients.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listGlobalClients adminRemoteSessions
 *
 * @remarks
 * List the global remote_session_clients registered with a global remote_session_issuer. Requires platform admin.
 */
export declare function adminRemoteSessionsListGlobalClients(client: GramCore, request: ListGlobalRemoteSessionClientsRequest, security?: ListGlobalRemoteSessionClientsSecurity | undefined, options?: RequestOptions): APIPromise<PageIterator<Result<ListGlobalRemoteSessionClientsResponse, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>, {
    cursor: string;
}>>;
//# sourceMappingURL=adminRemoteSessionsListGlobalClients.d.ts.map