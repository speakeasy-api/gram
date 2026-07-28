import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UserSessionClient } from "../models/components/usersessionclient.js";
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
  GetUserSessionClientRequest,
  GetUserSessionClientSecurity,
} from "../models/operations/getusersessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getUserSessionClient userSessionClients
 *
 * @remarks
 * Get a user_session_client by id.
 */
export declare function userSessionClientsGet(
  client: GramCore,
  request: GetUserSessionClientRequest,
  security?: GetUserSessionClientSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    UserSessionClient,
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
//# sourceMappingURL=userSessionClientsGet.d.ts.map
