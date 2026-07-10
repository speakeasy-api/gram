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
  DeleteRemoteSessionIssuerRequest,
  DeleteRemoteSessionIssuerSecurity,
} from "../models/operations/deleteremotesessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteRemoteSessionIssuer remoteSessionIssuers
 *
 * @remarks
 * Soft-delete a remote_session_issuer. Blocked if any remote_session_clients still reference it.
 */
export declare function remoteSessionIssuersDelete(
  client: GramCore,
  request: DeleteRemoteSessionIssuerRequest,
  security?: DeleteRemoteSessionIssuerSecurity | undefined,
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
//# sourceMappingURL=remoteSessionIssuersDelete.d.ts.map
