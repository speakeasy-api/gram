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
  ListUserSessionsRequest,
  ListUserSessionsResponse,
  ListUserSessionsSecurity,
} from "../models/operations/listusersessions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listUserSessions userSessions
 *
 * @remarks
 * List issued user_sessions in the caller's project. refresh_token_hash is never returned.
 */
export declare function userSessionsList(
  client: GramCore,
  request?: ListUserSessionsRequest | undefined,
  security?: ListUserSessionsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  PageIterator<
    Result<
      ListUserSessionsResponse,
      | ServiceError
      | GramError
      | ResponseValidationError
      | ConnectionError
      | RequestAbortedError
      | RequestTimeoutError
      | InvalidRequestError
      | UnexpectedClientError
      | SDKValidationError
    >,
    {
      cursor: string;
    }
  >
>;
//# sourceMappingURL=userSessionsList.d.ts.map
