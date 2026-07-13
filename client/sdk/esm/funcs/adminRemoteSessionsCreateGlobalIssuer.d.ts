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
  CreateGlobalRemoteSessionIssuerRequest,
  CreateGlobalRemoteSessionIssuerSecurity,
} from "../models/operations/createglobalremotesessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createGlobalIssuer adminRemoteSessions
 *
 * @remarks
 * Create a global remote_session_issuer (project_id NULL, organization_id NULL). Requires platform admin.
 */
export declare function adminRemoteSessionsCreateGlobalIssuer(
  client: GramCore,
  request: CreateGlobalRemoteSessionIssuerRequest,
  security?: CreateGlobalRemoteSessionIssuerSecurity | undefined,
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
//# sourceMappingURL=adminRemoteSessionsCreateGlobalIssuer.d.ts.map
