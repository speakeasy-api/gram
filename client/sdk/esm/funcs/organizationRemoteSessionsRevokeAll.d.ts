import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RevokeAllRemoteSessionsResult } from "../models/components/revokeallremotesessionsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RevokeAllOrganizationRemoteSessionClientSessionsRequest, RevokeAllOrganizationRemoteSessionClientSessionsSecurity } from "../models/operations/revokeallorganizationremotesessionclientsessions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * revokeAllClientSessions organizationRemoteSessions
 *
 * @remarks
 * Revoke (soft-delete) all remote_sessions minted against a remote_session_client in the caller's organization. Requires org:admin.
 */
export declare function organizationRemoteSessionsRevokeAll(client: GramCore, request: RevokeAllOrganizationRemoteSessionClientSessionsRequest, security?: RevokeAllOrganizationRemoteSessionClientSessionsSecurity | undefined, options?: RequestOptions): APIPromise<Result<RevokeAllRemoteSessionsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationRemoteSessionsRevokeAll.d.ts.map