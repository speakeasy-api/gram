import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RegisterRequest, RegisterSecurity } from "../models/operations/register.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * register auth
 *
 * @remarks
 * Register a new org for a user with their session information.
 */
export declare function authRegister(client: GramCore, request: RegisterRequest, security?: RegisterSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=authRegister.d.ts.map