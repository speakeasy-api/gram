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
  ListUserSessionConsentsRequest,
  ListUserSessionConsentsResponse,
  ListUserSessionConsentsSecurity,
} from "../models/operations/listusersessionconsents.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listUserSessionConsents userSessionConsents
 *
 * @remarks
 * List consent records for the caller's project.
 */
export declare function userSessionConsentsList(
  client: GramCore,
  request?: ListUserSessionConsentsRequest | undefined,
  security?: ListUserSessionConsentsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  PageIterator<
    Result<
      ListUserSessionConsentsResponse,
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
//# sourceMappingURL=userSessionConsentsList.d.ts.map
