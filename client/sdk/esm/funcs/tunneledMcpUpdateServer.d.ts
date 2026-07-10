import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TunneledMcpServer } from "../models/components/tunneledmcpserver.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateTunneledMcpServerRequest, UpdateTunneledMcpServerSecurity } from "../models/operations/updatetunneledmcpserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateServer tunneledMcp
 *
 * @remarks
 * Update a tunneled MCP server source
 */
export declare function tunneledMcpUpdateServer(client: GramCore, request: UpdateTunneledMcpServerRequest, security?: UpdateTunneledMcpServerSecurity | undefined, options?: RequestOptions): APIPromise<Result<TunneledMcpServer, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=tunneledMcpUpdateServer.d.ts.map