import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreateResponseBody } from "../models/components/createresponsebody.js";
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
  CreateChatSessionRequest,
  CreateChatSessionSecurity,
} from "../models/operations/createchatsession.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * create chatSessions
 *
 * @remarks
 * Creates a new chat session token
 */
export declare function chatSessionsCreate(
  client: GramCore,
  request: CreateChatSessionRequest,
  security?: CreateChatSessionSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    CreateResponseBody,
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
//# sourceMappingURL=chatSessionsCreate.d.ts.map
