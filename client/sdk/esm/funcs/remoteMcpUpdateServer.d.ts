import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteMcpServer } from "../models/components/remotemcpserver.js";
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
  UpdateRemoteMcpServerRequest,
  UpdateRemoteMcpServerSecurity,
} from "../models/operations/updateremotemcpserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateServer remoteMcp
 *
 * @remarks
 * Update a remote MCP server
 */
export declare function remoteMcpUpdateServer(
  client: GramCore,
  request: UpdateRemoteMcpServerRequest,
  security?: UpdateRemoteMcpServerSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RemoteMcpServer,
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
//# sourceMappingURL=remoteMcpUpdateServer.d.ts.map
