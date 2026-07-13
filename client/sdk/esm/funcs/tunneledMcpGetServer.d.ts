import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TunneledMcpServer } from "../models/components/tunneledmcpserver.js";
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
  GetTunneledMcpServerRequest,
  GetTunneledMcpServerSecurity,
} from "../models/operations/gettunneledmcpserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getServer tunneledMcp
 *
 * @remarks
 * Get a tunneled MCP server by ID
 */
export declare function tunneledMcpGetServer(
  client: GramCore,
  request: GetTunneledMcpServerRequest,
  security?: GetTunneledMcpServerSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    TunneledMcpServer,
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
//# sourceMappingURL=tunneledMcpGetServer.d.ts.map
