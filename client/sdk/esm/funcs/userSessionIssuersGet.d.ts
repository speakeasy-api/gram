import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UserSessionIssuer } from "../models/components/usersessionissuer.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  GetUserSessionIssuerRequest,
  GetUserSessionIssuerSecurity,
} from "../models/operations/getusersessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getUserSessionIssuer userSessionIssuers
 *
 * @remarks
 * Get a user_session_issuer by id or by slug. Provide exactly one.
 */
export declare function userSessionIssuersGet(
  client: GramCore,
  request?: GetUserSessionIssuerRequest | undefined,
  security?: GetUserSessionIssuerSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    UserSessionIssuer,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=userSessionIssuersGet.d.ts.map
