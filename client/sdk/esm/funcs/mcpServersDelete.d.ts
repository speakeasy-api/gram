import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteMcpServerRequest, DeleteMcpServerSecurity } from "../models/operations/deletemcpserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteMcpServer mcpServers
 *
 * @remarks
 * Delete an MCP server
 */
export declare function mcpServersDelete(client: GramCore, request: DeleteMcpServerRequest, security?: DeleteMcpServerSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=mcpServersDelete.d.ts.map