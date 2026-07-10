import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RemoveOrganizationRemoteSessionClientFromMcpServerRequest, RemoveOrganizationRemoteSessionClientFromMcpServerSecurity } from "../models/operations/removeorganizationremotesessionclientfrommcpserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * removeClientFromMcpServer organizationRemoteSessionClients
 *
 * @remarks
 * Detach a remote_session_client from an MCP server (clears the MCP server's user_session_issuer link) in the caller's organization. Requires org:admin.
 */
export declare function organizationRemoteSessionClientsRemoveFromMcpServer(client: GramCore, request: RemoveOrganizationRemoteSessionClientFromMcpServerRequest, security?: RemoveOrganizationRemoteSessionClientFromMcpServerSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationRemoteSessionClientsRemoveFromMcpServer.d.ts.map