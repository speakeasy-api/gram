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
  UpdateOrganizationRemoteSessionClientRequest,
  UpdateOrganizationRemoteSessionClientSecurity,
} from "../models/operations/updateorganizationremotesessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateClient organizationRemoteSessionClients
 *
 * @remarks
 * Update a remote_session_client's non-secret fields in the caller's organization. Requires org:admin.
 */
export declare function organizationRemoteSessionClientsUpdate(
  client: GramCore,
  request: UpdateOrganizationRemoteSessionClientRequest,
  security?: UpdateOrganizationRemoteSessionClientSecurity | undefined,
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
//# sourceMappingURL=organizationRemoteSessionClientsUpdate.d.ts.map
