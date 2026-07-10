import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListMcpServersResult } from "../models/components/listmcpserversresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListMcpServersRequest, ListMcpServersSecurity } from "../models/operations/listmcpservers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listMcpServers mcpServers
 *
 * @remarks
 * List MCP servers for a project. Accepts optional remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id filters to scope the result to a single backend; at most one filter may be supplied since the backends are mutually exclusive.
 */
export declare function mcpServersList(client: GramCore, request?: ListMcpServersRequest | undefined, security?: ListMcpServersSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListMcpServersResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=mcpServersList.d.ts.map