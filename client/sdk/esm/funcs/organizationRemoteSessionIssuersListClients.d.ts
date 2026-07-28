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
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listClients organizationRemoteSessionIssuers
 *
 * @remarks
 * List the remote_session_clients registered with a given issuer in the caller's organization, each with its MCP server attachment count. Requires org:read.
 */
export declare function organizationRemoteSessionIssuersListClients(
  client: GramCore,
  request: operations.ListOrganizationRemoteSessionClientsRequest,
  security?:
    | operations.ListOrganizationRemoteSessionClientsSecurity
    | undefined,
  options?: RequestOptions,
): APIPromise<
  PageIterator<
    Result<
      operations.ListOrganizationRemoteSessionClientsResponse,
      | errors.ServiceError
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
//# sourceMappingURL=organizationRemoteSessionIssuersListClients.d.ts.map
