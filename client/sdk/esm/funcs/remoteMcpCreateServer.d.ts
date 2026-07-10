import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteMcpServer } from "../models/components/remotemcpserver.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateRemoteMcpServerRequest, CreateRemoteMcpServerSecurity } from "../models/operations/createremotemcpserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createServer remoteMcp
 *
 * @remarks
 * Create a new remote MCP server
 */
export declare function remoteMcpCreateServer(client: GramCore, request: CreateRemoteMcpServerRequest, security?: CreateRemoteMcpServerSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteMcpServer, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteMcpCreateServer.d.ts.map