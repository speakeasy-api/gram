import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListOrganizationRemoteSessionClientSessionsRequest, ListOrganizationRemoteSessionClientSessionsResponse, ListOrganizationRemoteSessionClientSessionsSecurity } from "../models/operations/listorganizationremotesessionclientsessions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listClientSessions organizationRemoteSessions
 *
 * @remarks
 * List the remote_sessions minted against a remote_session_client in the caller's organization. access_token_encrypted and refresh_token_encrypted are never returned. Requires org:read.
 */
export declare function organizationRemoteSessionsList(client: GramCore, request: ListOrganizationRemoteSessionClientSessionsRequest, security?: ListOrganizationRemoteSessionClientSessionsSecurity | undefined, options?: RequestOptions): APIPromise<PageIterator<Result<ListOrganizationRemoteSessionClientSessionsResponse, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>, {
    cursor: string;
}>>;
//# sourceMappingURL=organizationRemoteSessionsList.d.ts.map