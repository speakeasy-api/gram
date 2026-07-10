import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteClient organizationRemoteSessionIssuers
 *
 * @remarks
 * Soft-delete a remote_session_client in the caller's organization. Cascades to the remote_sessions minted against it. Requires org:admin.
 */
export declare function organizationRemoteSessionIssuersDeleteClient(client: GramCore, request: operations.DeleteOrganizationRemoteSessionClientRequest, security?: operations.DeleteOrganizationRemoteSessionClientSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationRemoteSessionIssuersDeleteClient.d.ts.map