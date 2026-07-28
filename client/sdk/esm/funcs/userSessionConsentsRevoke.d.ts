import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  RevokeUserSessionConsentRequest,
  RevokeUserSessionConsentSecurity,
} from "../models/operations/revokeusersessionconsent.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * revokeUserSessionConsent userSessionConsents
 *
 * @remarks
 * Withdraw consent. Subsequent authorization requests for matching (subject, user_session_client) pairs re-prompt.
 */
export declare function userSessionConsentsRevoke(
  client: GramCore,
  request: RevokeUserSessionConsentRequest,
  security?: RevokeUserSessionConsentSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    void,
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
//# sourceMappingURL=userSessionConsentsRevoke.d.ts.map
