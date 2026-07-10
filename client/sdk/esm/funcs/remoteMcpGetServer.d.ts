import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteMcpServer } from "../models/components/remotemcpserver.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetRemoteMcpServerRequest, GetRemoteMcpServerSecurity } from "../models/operations/getremotemcpserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getServer remoteMcp
 *
 * @remarks
 * Get a remote MCP server by ID or slug. Exactly one of id or slug must be provided.
 */
export declare function remoteMcpGetServer(client: GramCore, request?: GetRemoteMcpServerRequest | undefined, security?: GetRemoteMcpServerSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteMcpServer, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteMcpGetServer.d.ts.map