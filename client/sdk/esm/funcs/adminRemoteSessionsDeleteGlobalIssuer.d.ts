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
  DeleteGlobalRemoteSessionIssuerRequest,
  DeleteGlobalRemoteSessionIssuerSecurity,
} from "../models/operations/deleteglobalremotesessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteGlobalIssuer adminRemoteSessions
 *
 * @remarks
 * Soft-delete a global remote_session_issuer. Blocked when any global remote_session_clients still reference it. Requires platform admin.
 */
export declare function adminRemoteSessionsDeleteGlobalIssuer(
  client: GramCore,
  request: DeleteGlobalRemoteSessionIssuerRequest,
  security?: DeleteGlobalRemoteSessionIssuerSecurity | undefined,
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
//# sourceMappingURL=adminRemoteSessionsDeleteGlobalIssuer.d.ts.map
