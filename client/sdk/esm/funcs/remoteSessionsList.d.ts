import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListRemoteSessionsRequest, ListRemoteSessionsResponse, ListRemoteSessionsSecurity } from "../models/operations/listremotesessions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listRemoteSessions remoteSessions
 *
 * @remarks
 * List remote_sessions in the caller's project. access_token_encrypted and refresh_token_encrypted are never returned — only metadata (access_expires_at, refresh_expires_at, scopes).
 */
export declare function remoteSessionsList(client: GramCore, request?: ListRemoteSessionsRequest | undefined, security?: ListRemoteSessionsSecurity | undefined, options?: RequestOptions): APIPromise<PageIterator<Result<ListRemoteSessionsResponse, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>, {
    cursor: string;
}>>;
//# sourceMappingURL=remoteSessionsList.d.ts.map