import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListUserSessionClientsRequest, ListUserSessionClientsResponse, ListUserSessionClientsSecurity } from "../models/operations/listusersessionclients.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listUserSessionClients userSessionClients
 *
 * @remarks
 * List user_session_clients in the caller's project.
 */
export declare function userSessionClientsList(client: GramCore, request?: ListUserSessionClientsRequest | undefined, security?: ListUserSessionClientsSecurity | undefined, options?: RequestOptions): APIPromise<PageIterator<Result<ListUserSessionClientsResponse, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>, {
    cursor: string;
}>>;
//# sourceMappingURL=userSessionClientsList.d.ts.map