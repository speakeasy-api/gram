import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RotateTunneledMcpServerKeyResult } from "../models/components/rotatetunneledmcpserverkeyresult.js";
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
  RotateTunneledMcpServerKeyRequest,
  RotateTunneledMcpServerKeySecurity,
} from "../models/operations/rotatetunneledmcpserverkey.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * rotateServerKey tunneledMcp
 *
 * @remarks
 * Rotate a tunneled MCP server source key. Returns the new tunnel key once.
 */
export declare function tunneledMcpRotateServerKey(
  client: GramCore,
  request: RotateTunneledMcpServerKeyRequest,
  security?: RotateTunneledMcpServerKeySecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    RotateTunneledMcpServerKeyResult,
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
//# sourceMappingURL=tunneledMcpRotateServerKey.d.ts.map
