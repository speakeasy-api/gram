import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSession } from "../models/components/remotesession.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RefreshOrganizationRemoteSessionRequest, RefreshOrganizationRemoteSessionSecurity } from "../models/operations/refreshorganizationremotesession.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * refreshSession organizationRemoteSessions
 *
 * @remarks
 * Force an upstream token refresh on a single remote_session in the caller's organization, regardless of current access-token expiry. Returns the updated remote_session so callers can reflect the new expiry without a refetch. Fails with a bad-request error when the session holds no refresh token. Requires org:admin.
 */
export declare function organizationRemoteSessionsRefresh(client: GramCore, request: RefreshOrganizationRemoteSessionRequest, security?: RefreshOrganizationRemoteSessionSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteSession, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationRemoteSessionsRefresh.d.ts.map