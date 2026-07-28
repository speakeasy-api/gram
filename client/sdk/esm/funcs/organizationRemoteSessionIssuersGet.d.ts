import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
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
  GetOrganizationRemoteSessionIssuerRequest,
  GetOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/getorganizationremotesessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getIssuer organizationRemoteSessionIssuers
 *
 * @remarks
 * Get any remote_session_issuer (organizational or project-specific) in the caller's organization by id. Requires org:read.
 */
export declare function organizationRemoteSessionIssuersGet(
  client: GramCore,
  request: GetOrganizationRemoteSessionIssuerRequest,
  security?: GetOrganizationRemoteSessionIssuerSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RemoteSessionIssuer,
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
//# sourceMappingURL=organizationRemoteSessionIssuersGet.d.ts.map
