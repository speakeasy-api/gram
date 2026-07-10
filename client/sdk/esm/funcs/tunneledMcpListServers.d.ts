import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListTunneledMcpServersResult } from "../models/components/listtunneledmcpserversresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListTunneledMcpServersRequest, ListTunneledMcpServersSecurity } from "../models/operations/listtunneledmcpservers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listServers tunneledMcp
 *
 * @remarks
 * List all tunneled MCP server sources for a project
 */
export declare function tunneledMcpListServers(client: GramCore, request?: ListTunneledMcpServersRequest | undefined, security?: ListTunneledMcpServersSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListTunneledMcpServersResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=tunneledMcpListServers.d.ts.map