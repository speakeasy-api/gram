import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UserSessionIssuer } from "../models/components/usersessionissuer.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateUserSessionIssuerRequest, CreateUserSessionIssuerSecurity } from "../models/operations/createusersessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createUserSessionIssuer userSessionIssuers
 *
 * @remarks
 * Create a new user_session_issuer.
 */
export declare function userSessionIssuersCreate(client: GramCore, request: CreateUserSessionIssuerRequest, security?: CreateUserSessionIssuerSecurity | undefined, options?: RequestOptions): APIPromise<Result<UserSessionIssuer, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=userSessionIssuersCreate.d.ts.map