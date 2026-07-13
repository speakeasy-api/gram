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
  CreateCimdRemoteSessionClientRequest,
  CreateCimdRemoteSessionClientSecurity,
} from "../models/operations/createcimdremotesessionclient.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createCimd remoteSessionClients
 *
 * @remarks
 * Register a remote_session_client in Client ID Metadata Document (CIMD) mode. Gram generates the client_id (the URL of a hosted client metadata document) and serves the document publicly; the client carries no secret and authenticates with token_endpoint_auth_method=none. The owning issuer must advertise client_id_metadata_document_supported.
 */
export declare function remoteSessionClientsCreateCimd(
  client: GramCore,
  request: CreateCimdRemoteSessionClientRequest,
  security?: CreateCimdRemoteSessionClientSecurity | undefined,
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
//# sourceMappingURL=remoteSessionClientsCreateCimd.d.ts.map
