import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { McpServer } from "../models/components/mcpserver.js";
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
  GetMcpServerRequest,
  GetMcpServerSecurity,
} from "../models/operations/getmcpserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getMcpServer mcpServers
 *
 * @remarks
 * Get an MCP server by ID or slug. Exactly one of id or slug must be provided.
 */
export declare function mcpServersGet(
  client: GramCore,
  request?: GetMcpServerRequest | undefined,
  security?: GetMcpServerSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    McpServer,
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
//# sourceMappingURL=mcpServersGet.d.ts.map
