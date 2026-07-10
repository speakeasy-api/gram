import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteTunneledMcpServerRequest, DeleteTunneledMcpServerSecurity } from "../models/operations/deletetunneledmcpserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteServer tunneledMcp
 *
 * @remarks
 * Delete a tunneled MCP server source
 */
export declare function tunneledMcpDeleteServer(client: GramCore, request: DeleteTunneledMcpServerRequest, security?: DeleteTunneledMcpServerSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=tunneledMcpDeleteServer.d.ts.map