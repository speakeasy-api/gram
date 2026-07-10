import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UserSessionIssuer } from "../models/components/usersessionissuer.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateUserSessionIssuerRequest, UpdateUserSessionIssuerSecurity } from "../models/operations/updateusersessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateUserSessionIssuer userSessionIssuers
 *
 * @remarks
 * Update fields on an existing user_session_issuer.
 */
export declare function userSessionIssuersUpdate(client: GramCore, request: UpdateUserSessionIssuerRequest, security?: UpdateUserSessionIssuerSecurity | undefined, options?: RequestOptions): APIPromise<Result<UserSessionIssuer, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=userSessionIssuersUpdate.d.ts.map