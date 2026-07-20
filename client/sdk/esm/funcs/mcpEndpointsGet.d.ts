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
  GetMcpEndpointRequest,
  GetMcpEndpointSecurity,
} from "../models/operations/getmcpendpoint.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getMcpEndpoint mcpEndpoints
 *
 * @remarks
 * Get an MCP endpoint by id or by (custom_domain_id, slug). Provide either id, or slug with an optional custom_domain_id — not both.
 */
export declare function mcpEndpointsGet(
  client: GramCore,
  request?: GetMcpEndpointRequest | undefined,
  security?: GetMcpEndpointSecurity | undefined,
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
//# sourceMappingURL=mcpEndpointsGet.d.ts.map
