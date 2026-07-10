import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TunneledMcpServerConnections } from "../models/components/tunneledmcpserverconnections.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListTunneledMcpServerConnectionsRequest, ListTunneledMcpServerConnectionsSecurity } from "../models/operations/listtunneledmcpserverconnections.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listServerConnections tunneledMcp
 *
 * @remarks
 * List live tunnel connections for a tunneled MCP server
 */
export declare function tunneledMcpListServerConnections(client: GramCore, request: ListTunneledMcpServerConnectionsRequest, security?: ListTunneledMcpServerConnectionsSecurity | undefined, options?: RequestOptions): APIPromise<Result<TunneledMcpServerConnections, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=tunneledMcpListServerConnections.d.ts.map