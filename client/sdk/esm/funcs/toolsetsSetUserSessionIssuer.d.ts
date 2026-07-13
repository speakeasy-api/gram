import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Toolset } from "../models/components/toolset.js";
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
  SetToolsetUserSessionIssuerRequest,
  SetToolsetUserSessionIssuerSecurity,
} from "../models/operations/settoolsetusersessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * setUserSessionIssuer toolsets
 *
 * @remarks
 * Link a toolset to a user_session_issuer (or pass null to unlink). The user_session_issuer must already exist in the caller's project.
 */
export declare function toolsetsSetUserSessionIssuer(
  client: GramCore,
  request: SetToolsetUserSessionIssuerRequest,
  security?: SetToolsetUserSessionIssuerSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    Toolset,
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
//# sourceMappingURL=toolsetsSetUserSessionIssuer.d.ts.map
