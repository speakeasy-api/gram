import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
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
  UpdateGlobalRemoteSessionClientRequest,
  UpdateGlobalRemoteSessionClientSecurity,
} from "../models/operations/updateglobalremotesessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateGlobalClient adminRemoteSessions
 *
 * @remarks
 * Rotate the client_secret or change non-issuer settings on a global remote_session_client. Requires platform admin.
 */
export declare function adminRemoteSessionsUpdateGlobalClient(
  client: GramCore,
  request: UpdateGlobalRemoteSessionClientRequest,
  security?: UpdateGlobalRemoteSessionClientSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RemoteSessionClient,
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
//# sourceMappingURL=adminRemoteSessionsUpdateGlobalClient.d.ts.map
