import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteOrganizationRemoteSessionClientRequest, DeleteOrganizationRemoteSessionClientSecurity } from "../models/operations/deleteorganizationremotesessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteClient organizationRemoteSessionClients
 *
 * @remarks
 * Soft-delete a remote_session_client in the caller's organization. Cascades to the remote_sessions minted against it. Requires org:admin.
 */
export declare function organizationRemoteSessionClientsDelete(client: GramCore, request: DeleteOrganizationRemoteSessionClientRequest, security?: DeleteOrganizationRemoteSessionClientSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationRemoteSessionClientsDelete.d.ts.map