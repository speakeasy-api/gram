import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  RevokeChatSessionRequest,
  RevokeChatSessionSecurity,
} from "../models/operations/revokechatsession.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * revoke chatSessions
 *
 * @remarks
 * Revokes an existing chat session token
 */
export declare function chatSessionsRevoke(
  client: GramCore,
  request: RevokeChatSessionRequest,
  security?: RevokeChatSessionSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    void,
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
//# sourceMappingURL=chatSessionsRevoke.d.ts.map
