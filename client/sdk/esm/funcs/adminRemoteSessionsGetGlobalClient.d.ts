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
  GetGlobalRemoteSessionClientRequest,
  GetGlobalRemoteSessionClientSecurity,
} from "../models/operations/getglobalremotesessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getGlobalClient adminRemoteSessions
 *
 * @remarks
 * Get a global remote_session_client by id. Requires platform admin.
 */
export declare function adminRemoteSessionsGetGlobalClient(
  client: GramCore,
  request: GetGlobalRemoteSessionClientRequest,
  security?: GetGlobalRemoteSessionClientSecurity | undefined,
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
//# sourceMappingURL=adminRemoteSessionsGetGlobalClient.d.ts.map
