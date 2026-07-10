import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { MoveOrganizationRemoteSessionIssuerRequest, MoveOrganizationRemoteSessionIssuerSecurity } from "../models/operations/moveorganizationremotesessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * moveIssuer organizationRemoteSessionIssuers
 *
 * @remarks
 * Re-scope a remote_session_issuer in the caller's organization: provide a project_id (which must belong to the organization) to make it project-specific, or omit it to make it organization-level (project_id NULL, inherited by every project). Requires org:admin.
 */
export declare function organizationRemoteSessionIssuersMove(client: GramCore, request: MoveOrganizationRemoteSessionIssuerRequest, security?: MoveOrganizationRemoteSessionIssuerSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteSessionIssuer, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationRemoteSessionIssuersMove.d.ts.map