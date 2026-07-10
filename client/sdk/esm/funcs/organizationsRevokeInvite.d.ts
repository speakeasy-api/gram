import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RevokeInviteRequest, RevokeInviteSecurity } from "../models/operations/revokeinvite.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * revokeInvite organizations
 *
 * @remarks
 * Revoke a pending WorkOS invitation.
 */
export declare function organizationsRevokeInvite(client: GramCore, request: RevokeInviteRequest, security?: RevokeInviteSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationsRevokeInvite.d.ts.map