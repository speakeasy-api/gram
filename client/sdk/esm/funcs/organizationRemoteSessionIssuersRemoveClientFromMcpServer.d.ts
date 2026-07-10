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
/**
 * removeClientFromMcpServer organizationRemoteSessionIssuers
 *
 * @remarks
 * Detach a remote_session_client from an MCP server (clears the MCP server's user_session_issuer link) in the caller's organization. Requires org:admin.
 */
export declare function organizationRemoteSessionIssuersRemoveClientFromMcpServer(
  client: GramCore,
  request: operations.RemoveOrganizationRemoteSessionClientFromMcpServerRequest,
  security?:
    | operations.RemoveOrganizationRemoteSessionClientFromMcpServerSecurity
    | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    void,
    | errors.ServiceError
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
//# sourceMappingURL=organizationRemoteSessionIssuersRemoveClientFromMcpServer.d.ts.map
