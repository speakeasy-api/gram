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
  CreateMcpEndpointRequest,
  CreateMcpEndpointSecurity,
} from "../models/operations/createmcpendpoint.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createMcpEndpoint mcpEndpoints
 *
 * @remarks
 * Create a new MCP endpoint for an MCP server
 */
export declare function mcpEndpointsCreate(
  client: GramCore,
  request: CreateMcpEndpointRequest,
  security?: CreateMcpEndpointSecurity | undefined,
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
//# sourceMappingURL=mcpEndpointsCreate.d.ts.map
