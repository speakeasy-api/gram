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
  UpdateRemoteSessionClientRequest,
  UpdateRemoteSessionClientSecurity,
} from "../models/operations/updateremotesessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateRemoteSessionClient remoteSessionClients
 *
 * @remarks
 * Rotate the client_secret or change the non-issuer settings on an existing remote_session_client. Issuer attachments are managed via attachUserSessionIssuer / detachUserSessionIssuer.
 */
export declare function remoteSessionClientsUpdate(
  client: GramCore,
  request: UpdateRemoteSessionClientRequest,
  security?: UpdateRemoteSessionClientSecurity | undefined,
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
//# sourceMappingURL=remoteSessionClientsUpdate.d.ts.map
