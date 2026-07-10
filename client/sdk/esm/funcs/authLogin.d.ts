import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { AuthLoginRequest, AuthLoginResponse } from "../models/operations/authlogin.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * login auth
 *
 * @remarks
 * Proxies to auth login through speakeasy oidc.
 */
export declare function authLogin(client: GramCore, request?: AuthLoginRequest | undefined, options?: RequestOptions): APIPromise<Result<AuthLoginResponse | undefined, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=authLogin.d.ts.map