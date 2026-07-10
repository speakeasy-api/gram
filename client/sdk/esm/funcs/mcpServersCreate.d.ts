import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { McpServer } from "../models/components/mcpserver.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateMcpServerRequest, CreateMcpServerSecurity } from "../models/operations/createmcpserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createMcpServer mcpServers
 *
 * @remarks
 * Create a new MCP server
 */
export declare function mcpServersCreate(client: GramCore, request: CreateMcpServerRequest, security?: CreateMcpServerSecurity | undefined, options?: RequestOptions): APIPromise<Result<McpServer, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=mcpServersCreate.d.ts.map