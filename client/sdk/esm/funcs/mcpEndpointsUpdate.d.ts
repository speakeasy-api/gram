import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { McpEndpoint } from "../models/components/mcpendpoint.js";
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
  UpdateMcpEndpointRequest,
  UpdateMcpEndpointSecurity,
} from "../models/operations/updatemcpendpoint.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateMcpEndpoint mcpEndpoints
 *
 * @remarks
 * Update an MCP endpoint. This is a full-record replace: fields omitted from the request become null on the stored record. The id, mcp_server_id, and slug fields are required.
 */
export declare function mcpEndpointsUpdate(
  client: GramCore,
  request: UpdateMcpEndpointRequest,
  security?: UpdateMcpEndpointSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    McpEndpoint,
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
//# sourceMappingURL=mcpEndpointsUpdate.d.ts.map
