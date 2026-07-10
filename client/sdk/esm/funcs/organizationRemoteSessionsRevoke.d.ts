import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RevokeOrganizationRemoteSessionRequest, RevokeOrganizationRemoteSessionSecurity } from "../models/operations/revokeorganizationremotesession.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * revokeSession organizationRemoteSessions
 *
 * @remarks
 * Revoke (soft-delete) a single remote_session in the caller's organization. Requires org:admin.
 */
export declare function organizationRemoteSessionsRevoke(client: GramCore, request: RevokeOrganizationRemoteSessionRequest, security?: RevokeOrganizationRemoteSessionSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationRemoteSessionsRevoke.d.ts.map