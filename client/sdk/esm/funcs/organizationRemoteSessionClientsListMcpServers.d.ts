import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListOrganizationMcpServersResult } from "../models/components/listorganizationmcpserversresult.js";
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
  ListOrganizationRemoteSessionClientMcpServersRequest,
  ListOrganizationRemoteSessionClientMcpServersSecurity,
} from "../models/operations/listorganizationremotesessionclientmcpservers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listClientMcpServers organizationRemoteSessionClients
 *
 * @remarks
 * List the MCP servers a remote_session_client is attached to (resolved through user_session_issuers) in the caller's organization. Requires org:read.
 */
export declare function organizationRemoteSessionClientsListMcpServers(
  client: GramCore,
  request: ListOrganizationRemoteSessionClientMcpServersRequest,
  security?: ListOrganizationRemoteSessionClientMcpServersSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListOrganizationMcpServersResult,
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
//# sourceMappingURL=organizationRemoteSessionClientsListMcpServers.d.ts.map
