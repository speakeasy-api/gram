import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateIssuer organizationRemoteSessionIssuers
 *
 * @remarks
 * Update any remote_session_issuer (organizational or project-specific) in the caller's organization. Requires org:admin.
 */
export declare function organizationRemoteSessionIssuersUpdateIssuer(
  client: GramCore,
  request: operations.UpdateOrganizationRemoteSessionIssuerRequest,
  security?:
    | operations.UpdateOrganizationRemoteSessionIssuerSecurity
    | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    components.RemoteSessionIssuer,
    | errors.ServiceError
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
//# sourceMappingURL=organizationRemoteSessionIssuersUpdateIssuer.d.ts.map
