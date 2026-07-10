import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListMcpEndpointsResult } from "../models/components/listmcpendpointsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListMcpEndpointsRequest, ListMcpEndpointsSecurity } from "../models/operations/listmcpendpoints.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listMcpEndpoints mcpEndpoints
 *
 * @remarks
 * List MCP endpoints for a project. Optionally filter to only those associated with a specific MCP server.
 */
export declare function mcpEndpointsList(client: GramCore, request?: ListMcpEndpointsRequest | undefined, security?: ListMcpEndpointsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListMcpEndpointsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=mcpEndpointsList.d.ts.map