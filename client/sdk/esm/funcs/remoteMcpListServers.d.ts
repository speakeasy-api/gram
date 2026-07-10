import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListServersResult } from "../models/components/listserversresult.js";
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
  ListRemoteMcpServersRequest,
  ListRemoteMcpServersSecurity,
} from "../models/operations/listremotemcpservers.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listServers remoteMcp
 *
 * @remarks
 * List all remote MCP servers for a project
 */
export declare function remoteMcpListServers(
  client: GramCore,
  request?: ListRemoteMcpServersRequest | undefined,
  security?: ListRemoteMcpServersSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListServersResult,
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
//# sourceMappingURL=remoteMcpListServers.d.ts.map
