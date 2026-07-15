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
  DeleteOrganizationRemoteSessionIssuerRequest,
  DeleteOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/deleteorganizationremotesessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteIssuer organizationRemoteSessionIssuers
 *
 * @remarks
 * Soft-delete any remote_session_issuer (organizational or project-specific) in the caller's organization. Blocked when any remote_session_clients still reference it. Requires org:admin.
 */
export declare function organizationRemoteSessionIssuersDelete(
  client: GramCore,
  request: DeleteOrganizationRemoteSessionIssuerRequest,
  security?: DeleteOrganizationRemoteSessionIssuerSecurity | undefined,
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
//# sourceMappingURL=organizationRemoteSessionIssuersDelete.d.ts.map
