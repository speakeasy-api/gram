import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreateTunneledMcpServerResult } from "../models/components/createtunneledmcpserverresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateTunneledMcpServerRequest, CreateTunneledMcpServerSecurity } from "../models/operations/createtunneledmcpserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createServer tunneledMcp
 *
 * @remarks
 * Create a new tunneled MCP server source. Returns the tunnel key once.
 */
export declare function tunneledMcpCreateServer(client: GramCore, request: CreateTunneledMcpServerRequest, security?: CreateTunneledMcpServerSecurity | undefined, options?: RequestOptions): APIPromise<Result<CreateTunneledMcpServerResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=tunneledMcpCreateServer.d.ts.map