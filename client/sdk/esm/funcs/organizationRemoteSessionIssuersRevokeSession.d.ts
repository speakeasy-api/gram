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
 * revokeSession organizationRemoteSessionIssuers
 *
 * @remarks
 * Revoke (soft-delete) a single remote_session in the caller's organization. Requires org:admin.
 */
export declare function organizationRemoteSessionIssuersRevokeSession(client: GramCore, request: operations.RevokeOrganizationRemoteSessionRequest, security?: operations.RevokeOrganizationRemoteSessionSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationRemoteSessionIssuersRevokeSession.d.ts.map