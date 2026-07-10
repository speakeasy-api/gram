import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolFiltersResult } from "../models/components/listtoolfiltersresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListMcpServerToolFiltersRequest, ListMcpServerToolFiltersSecurity } from "../models/operations/listmcpservertoolfilters.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listToolFilters mcpServers
 *
 * @remarks
 * List the tool filter scopes (tags) available on an MCP server and the tools under each, including tools excluded from all filters. Exactly one of id or slug must be provided. Read-only; reflects the explicit tool variations group resolved from the chain (mcp_servers then toolsets), deriving effective tags with the same logic as the runtime ?tags= filter. Returns filtering disabled when no explicit group is set.
 */
export declare function mcpServersListToolFilters(client: GramCore, request?: ListMcpServerToolFiltersRequest | undefined, security?: ListMcpServerToolFiltersSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListToolFiltersResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=mcpServersListToolFilters.d.ts.map