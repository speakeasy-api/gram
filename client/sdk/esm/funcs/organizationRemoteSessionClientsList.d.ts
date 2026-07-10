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
  ListOrganizationRemoteSessionClientsRequest,
  ListOrganizationRemoteSessionClientsResponse,
  ListOrganizationRemoteSessionClientsSecurity,
} from "../models/operations/listorganizationremotesessionclients.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listClients organizationRemoteSessionClients
 *
 * @remarks
 * List the remote_session_clients registered with a given issuer in the caller's organization, each with its MCP server attachment count. Requires org:read.
 */
export declare function organizationRemoteSessionClientsList(
  client: GramCore,
  request: ListOrganizationRemoteSessionClientsRequest,
  security?: ListOrganizationRemoteSessionClientsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  PageIterator<
    Result<
      ListOrganizationRemoteSessionClientsResponse,
      | ServiceError
      | GramError
      | ResponseValidationError
      | ConnectionError
      | RequestAbortedError
      | RequestTimeoutError
      | InvalidRequestError
      | UnexpectedClientError
      | SDKValidationError
    >,
    {
      cursor: string;
    }
  >
>;
//# sourceMappingURL=organizationRemoteSessionClientsList.d.ts.map
